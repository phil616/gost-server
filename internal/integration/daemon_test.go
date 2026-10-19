package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/altasci/altascitunnel/internal/agent"
	"github.com/altasci/altascitunnel/internal/control"
	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type controlEdge struct{ *edge.Engine }

func (e controlEdge) Apply(_ context.Context, p model.Policy) error { return e.Engine.Apply(p) }
func (e controlEdge) Snapshot(context.Context) (model.RuntimeSnapshot, error) {
	return e.Engine.Snapshot(), nil
}
func TestDaemonWSSRegistrationDataAndControlOutage(t *testing.T) {
	if os.Getenv("GOST_BINARY") == "" {
		t.Skip("set GOST_BINARY")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine, _ := edge.New("")
	edgeAddr := freeAddress(t)
	if e := engine.Start(ctx, edgeAddr); e != nil {
		t.Fatal(e)
	}
	vault, _ := secure.New(bytes.Repeat([]byte{4}, 32))
	repo := store.NewMemory()
	server := control.New(repo, vault, controlEdge{engine}, "")
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	if os.Getenv("ALTASCI_BROWSER") == "1" {
		server.WebDir = filepath.Join(root, "web", "dist")
		server.CookieSecure = false // isolated loopback browser test only
	}
	if e := server.InitAdmin(ctx, "admin", "integration-password"); e != nil {
		t.Fatal(e)
	}
	target, _ := url.Parse("http://" + edgeAddr)
	mux := http.NewServeMux()
	mux.Handle("/ws", httputil.NewSingleHostReverseProxy(target))
	mux.Handle("/", server.Handler())
	tlsServer := httptest.NewTLSServer(mux)
	defer tlsServer.Close()
	server.PublicURL = tlsServer.URL
	server.Start(ctx)
	defer server.Close()
	certPath := filepath.Join(t.TempDir(), "ca.pem")
	if e := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw}), 0600); e != nil {
		t.Fatal(e)
	}
	browser := tlsServer.Client()
	browser.Jar, _ = cookiejar.New(nil)
	csrf := ""
	request := func(method, path string, body any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, tlsServer.URL+"/api/v1"+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		res, e := browser.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
		}
		var out map[string]any
		json.Unmarshal(data, &out)
		return out
	}
	login := request("POST", "/auth/login", map[string]string{"username": "admin", "password": "integration-password"})
	csrf = login["csrf"].(string)
	en := request("POST", "/enrollments", map[string]string{"name": "live-daemon"})
	directory := t.TempDir()
	daemon := &agent.Daemon{Directory: directory, Server: "wss" + strings.TrimPrefix(tlsServer.URL, "https"), CAFile: certPath}
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon shutdown timeout")
		}
	}()
	local := agent.LocalClient(directory)
	waitFor(t, func() bool {
		r, e := local.Get("http://local/status")
		if e != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == 200
	})
	b, _ := json.Marshal(map[string]string{"token": en["token"].(string)})
	res, e := local.Post("http://local/register", "application/json", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatal("local registration rejected", res.StatusCode)
	}
	var clientID string
	waitFor(t, func() bool {
		st, _ := repo.Read(ctx)
		for _, c := range st.Clients {
			clientID = c.ID
			return true
		}
		return false
	})
	origin, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer origin.Close()
	go func() {
		for {
			c, e := origin.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	host, port, _ := net.SplitHostPort(origin.Addr().String())
	var n int
	json.Unmarshal([]byte(port), &n)
	created := request("POST", "/clients/"+clientID+"/tunnels", map[string]any{"protocol": "tcp", "target_ip": host, "target_port": n})
	tid := created["resource_id"].(string)
	waitFor(t, func() bool {
		for _, s := range engine.Snapshot().Sessions {
			if s.TunnelID == tid && s.Kind == "provider" && s.Connected {
				return true
			}
		}
		return false
	})
	cred := request("POST", "/tunnels/"+tid+"/access-credentials", map[string]any{})
	cid := cred["resource_id"].(string)
	secret := request("POST", "/access-credentials/"+cid+"/reveal", map[string]any{})
	waitFor(t, func() bool { st, _ := repo.Read(ctx); return engine.Policy().Revision == st.Meta.PolicyRevision })
	u, _ := url.Parse(tlsServer.URL)
	q := url.Values{"path": {"/ws"}, "tunnel.id": {tid}, "secure": {"true"}, "caFile": {certPath}, "ws.keepalive": {"true"}}
	localAddr := freeAddress(t)
	native(t, "-L", "tcp://"+localAddr+"/203.0.113.1:1234", "-F", "tunnel+wss://"+secret["username"].(string)+":"+secret["secret"].(string)+"@"+u.Host+"?"+q.Encode())
	var conn net.Conn
	waitFor(t, func() bool { conn, e = net.DialTimeout("tcp", localAddr, time.Second); return e == nil })
	defer conn.Close()
	echo := func() {
		t.Helper()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, e := conn.Write([]byte("alive")); e != nil {
			t.Fatal(e)
		}
		buffer := make([]byte, 5)
		if _, e := io.ReadFull(conn, buffer); e != nil || string(buffer) != "alive" {
			t.Fatal("echo", e)
		}
	}
	echo()
	if os.Getenv("ALTASCI_BROWSER") == "1" {
		// The entire server, identity and authorization store are disposable test
		// fixtures. No administrator or service is installed on the user's host.
		browserServer := httptest.NewServer(mux)
		defer browserServer.Close()
		cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "browser-smoke.mjs"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "SMOKE_ORIGIN="+browserServer.URL, "SMOKE_USERNAME=admin", "SMOKE_PASSWORD=integration-password", "SMOKE_MUTATIONS=1", "SMOKE_CLIENT_NAME=live-daemon", "SMOKE_TARGET_PORT="+port)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("browser: %v\n%s", err, output)
		} else {
			t.Log(string(output))
		}
	}
	server.Close()
	time.Sleep(200 * time.Millisecond)
	echo()
	server.Start(ctx)
	request("DELETE", "/access-credentials/"+cid, map[string]any{})
	waitFor(t, func() bool {
		for _, s := range engine.Snapshot().Sessions {
			if s.CredentialID == cid {
				return false
			}
		}
		return true
	})
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, e = conn.Read(make([]byte, 1)); e == nil {
		t.Fatal("revocation did not terminate existing WSS data session")
	}
}
