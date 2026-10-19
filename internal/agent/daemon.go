package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/altasci/altascitunnel/internal/localfile"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/retry"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

type Identity struct {
	ClientID        string `json:"client_id"`
	PrivateKey      string `json:"private_key"`
	RequestID       string `json:"request_id"`
	EnrollmentToken string `json:"enrollment_token,omitempty"`
	Server          string `json:"server"`
}
type Daemon struct {
	Directory, Server, CAFile string
	AllowInsecureLocal        bool
	mu                        sync.Mutex
	identity                  Identity
	config                    model.ClientConfig
	runtime                   *Runtime
	wake                      chan struct{}
	management                *retry.Policy
	state                     string
	lastError                 string
}

func (d *Daemon) Run(ctx context.Context) error {
	unlock, e := acquireLock(d.Directory)
	if e != nil {
		return e
	}
	defer unlock()
	if e = os.MkdirAll(d.Directory, 0700); e != nil {
		return e
	}
	d.wake = make(chan struct{}, 1)
	d.state = "unregistered"
	if b, e := os.ReadFile(filepath.Join(d.Directory, "identity.json")); e == nil {
		if e = json.Unmarshal(b, &d.identity); e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if d.identity.Server != "" {
		if d.Server != "" && d.Server != d.identity.Server {
			return errors.New("server differs from registered identity")
		}
		d.Server = d.identity.Server
	}
	if d.Server == "" {
		d.Server = "wss://tunnel.example.com"
	}
	u, e := url.Parse(d.Server)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("invalid server URL")
	}
	if u.Scheme != "wss" && !(d.AllowInsecureLocal && u.Scheme == "ws" && isLoopback(u.Hostname())) {
		return errors.New("server must use wss (ws is only allowed by explicit loopback test flag)")
	}
	dataURL := *u
	dataURL.Path = "/ws"
	d.runtime = NewRuntime(ctx, dataURL.String(), d.CAFile, d.Directory)
	defer d.runtime.Close()
	if b, e := os.ReadFile(filepath.Join(d.Directory, "config.json")); e == nil {
		if e = json.Unmarshal(b, &d.config); e != nil {
			return e
		}
		if _, e = d.runtime.Apply(ctx, d.config); e != nil {
			return e
		}
	}
	var saved retry.State
	if b, e := os.ReadFile(filepath.Join(d.Directory, "management-retry.json")); e == nil {
		_ = json.Unmarshal(b, &saved)
	}
	d.management = retry.New(saved)
	d.management.Save = func(s retry.State) {
		if e := localfile.Write(filepath.Join(d.Directory, "management-retry.json"), s); e != nil {
			slog.Error("persist management retry", "error", e)
		}
	}
	ln, e := listenIPC(d.Directory)
	if e != nil {
		return e
	}
	defer ln.Close()
	server := &http.Server{Handler: d.localHandler(), ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(ln)
	defer server.Close()
	for {
		d.mu.Lock()
		id := d.identity
		d.mu.Unlock()
		if id.PrivateKey == "" {
			select {
			case <-ctx.Done():
				return nil
			case <-d.wake:
				continue
			}
		}
		if e = d.management.Await(ctx); e != nil {
			return nil
		}
		d.setState("connecting", nil)
		e = d.connect(ctx)
		if ctx.Err() != nil {
			return nil
		}
		d.management.Failed()
		d.setState("backoff", e)
	}
}
func isLoopback(host string) bool {
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}
func (d *Daemon) setState(state string, e error) {
	d.mu.Lock()
	d.state = state
	if e != nil {
		d.lastError = e.Error()
	} else {
		d.lastError = ""
	}
	d.mu.Unlock()
}
func (d *Daemon) persistIdentity() error {
	return localfile.Write(filepath.Join(d.Directory, "identity.json"), d.identity)
}
func (d *Daemon) Register(token string, servers ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.identity.ClientID != "" {
		return errors.New("device already registered")
	}
	if token == "" {
		return errors.New("token required")
	}
	if len(servers) > 0 && servers[0] != "" && NormalizeServer(servers[0]) != d.Server {
		if d.identity.EnrollmentToken != "" {
			return errors.New("cannot change server during pending enrollment")
		}
		u, e := url.Parse(NormalizeServer(servers[0]))
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Scheme != "wss" && !(d.AllowInsecureLocal && u.Scheme == "ws" && isLoopback(u.Hostname())) {
			return errors.New("server must be a WSS origin")
		}
		d.Server = NormalizeServer(servers[0])
		u.Path = "/ws"
		d.runtime.applyMu.Lock()
		d.runtime.endpoint = u.String()
		d.runtime.applyMu.Unlock()
	}
	if d.identity.PrivateKey == "" {
		_, priv, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		d.identity.PrivateKey = base64.RawURLEncoding.EncodeToString(priv)
		d.identity.RequestID = uuid.NewString()
	}
	d.identity.EnrollmentToken = token
	d.identity.Server = d.Server
	if e := d.persistIdentity(); e != nil {
		return e
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return nil
}
func (d *Daemon) connect(ctx context.Context) error {
	d.mu.Lock()
	identity := d.identity
	d.mu.Unlock()
	priv, e := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	if e != nil || len(priv) != ed25519.PrivateKeySize {
		return errors.New("invalid persisted device key")
	}
	u, _ := url.Parse(identity.Server)
	u.Path = "/agent/v1/ws"
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if d.CAFile != "" {
		b, e := os.ReadFile(d.CAFile)
		if e != nil {
			return e
		}
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(b) {
			return errors.New("invalid CA file")
		}
		tr.TLSClientConfig.RootCAs = pool
	}
	defer tr.CloseIdleConnections()
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, e := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	cancel()
	if e != nil {
		return errors.New("management connection failed")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	handshake, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var m model.Message
	if e = wsjson.Read(handshake, conn, &m); e != nil {
		return e
	}
	var challenge struct {
		Nonce string `json:"nonce"`
	}
	if m.V != 1 || m.Type != "challenge" || json.Unmarshal(m.Payload, &challenge) != nil || challenge.Nonce == "" {
		return errors.New("invalid server challenge")
	}
	payload := map[string]any{"client_id": identity.ClientID, "request_id": identity.RequestID, "token": identity.EnrollmentToken, "public_key": base64.RawURLEncoding.EncodeToString(ed25519.PrivateKey(priv).Public().(ed25519.PublicKey)), "signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(challenge.Nonce))), "os": runtime.GOOS, "arch": runtime.GOARCH, "version": "0.1.0"}
	kind := "authenticate"
	if identity.ClientID == "" {
		kind = "register"
	}
	if e = send(handshake, conn, kind, payload); e != nil {
		return e
	}
	if e = wsjson.Read(handshake, conn, &m); e != nil {
		return e
	}
	if m.V != 1 || m.Type != "authenticated" {
		return errors.New("management authentication rejected")
	}
	var authenticated struct {
		ClientID string `json:"client_id"`
	}
	if e = json.Unmarshal(m.Payload, &authenticated); e != nil || authenticated.ClientID == "" {
		return errors.New("invalid authenticated identity")
	}
	d.mu.Lock()
	d.identity.ClientID = authenticated.ClientID
	d.identity.EnrollmentToken = ""
	e = d.persistIdentity()
	d.mu.Unlock()
	if e != nil {
		return e
	}
	d.management.Reset()
	d.setState("online", nil)
	session, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	incoming := make(chan model.Message, 8)
	readErrors := make(chan error, 1)
	go func() {
		for {
			var msg model.Message
			if e := wsjson.Read(session, conn, &msg); e != nil {
				readErrors <- e
				return
			}
			select {
			case incoming <- msg:
			case <-session.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	last := time.Now()
	status := func() map[string]any {
		received, applied := d.runtime.Revisions()
		return map[string]any{"received_revision": received, "applied_revision": applied, "runtime": d.runtime.Snapshot()}
	}
	if e = send(session, conn, "hello", status()); e != nil {
		return e
	}
	for {
		select {
		case <-session.Done():
			return session.Err()
		case e := <-readErrors:
			return e
		case <-ticker.C:
			if time.Since(last) > 45*time.Second {
				return errors.New("management heartbeat timeout")
			}
			if e = send(session, conn, "runtime_snapshot", status()); e != nil {
				return e
			}
		case <-d.runtime.Events():
			if e = send(session, conn, "runtime_event", status()); e != nil {
				return e
			}
		case msg := <-incoming:
			last = time.Now()
			if msg.V != 1 {
				return errors.New("unsupported management protocol")
			}
			switch msg.Type {
			case "heartbeat":
			case "desired_config":
				var cfg model.ClientConfig
				if e = json.Unmarshal(msg.Payload, &cfg); e != nil {
					return e
				}
				if cfg.ClientID != authenticated.ClientID || cfg.Validate() != nil {
					return errors.New("invalid desired configuration")
				}
				d.mu.Lock()
				old := d.config
				if cfg.Revision < old.Revision {
					d.mu.Unlock()
					continue
				}
				if cfg.Revision == old.Revision && old.Hash != "" && cfg.Hash != old.Hash {
					d.mu.Unlock()
					return errors.New("config revision/hash conflict")
				}
				if e = localfile.Write(filepath.Join(d.Directory, "config.json"), cfg); e != nil {
					d.mu.Unlock()
					return e
				}
				d.config = cfg
				d.mu.Unlock()
				result, e := d.runtime.Apply(session, cfg)
				if e != nil {
					return e
				}
				ack := status()
				ack["errors"] = result.Errors
				if e = send(session, conn, "config_ack", ack); e != nil {
					return e
				}
			case "error":
				return errors.New("server rejected device session")
			default:
				return fmt.Errorf("unsupported management message %s", msg.Type)
			}
		}
	}
}
func send(ctx context.Context, c *websocket.Conn, kind string, payload any) error {
	b, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return wsjson.Write(ctx, c, model.Message{V: 1, Type: kind, ID: uuid.NewString(), Payload: b})
}
func (d *Daemon) localHandler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Token  string `json:"token"`
			Server string `json:"server"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if e := d.Register(b.Token, b.Server); e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		w.WriteHeader(202)
	})
	m.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		v := map[string]any{"client_id": d.identity.ClientID, "state": d.state, "error": d.lastError}
		d.mu.Unlock()
		v["runtime"] = d.runtime.Snapshot()
		v["retry"] = d.management.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	})
	return m
}
func LocalClient(directory string) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dialIPC(ctx, directory) }}}
}
func DefaultDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "AltasCITunnel")
	}
	return "/var/lib/altascitunnel"
}
func NormalizeServer(s string) string { return strings.TrimRight(s, "/") }
