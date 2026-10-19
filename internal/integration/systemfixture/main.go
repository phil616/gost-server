// systemfixture is a disposable, loopback-only acceptance fixture. It is not
// shipped as a production server. The database is deliberately in memory;
// PostgreSQL acceptance is performed separately on Linux.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/altasci/altascitunnel/internal/control"
	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
)

type adapter struct{ *edge.Engine }

func (a adapter) Apply(_ context.Context, p model.Policy) error { return a.Engine.Apply(p) }
func (a adapter) Snapshot(context.Context) (model.RuntimeSnapshot, error) {
	return a.Engine.Snapshot(), nil
}

func main() {
	dir := flag.String("output", "fixture", "private fixture output directory")
	flag.Parse()
	if err := run(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	write := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		tmp := filepath.Join(dir, name+".tmp")
		if err = os.WriteFile(tmp, b, 0600); err != nil {
			return err
		}
		return os.Rename(tmp, filepath.Join(dir, name))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e, err := edge.New("")
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := l.Addr().String()
	l.Close()
	if err = e.Start(ctx, addr); err != nil {
		return err
	}
	vault, _ := secure.New(bytes.Repeat([]byte{7}, 32))
	repo := store.NewMemory()
	c := control.New(repo, vault, adapter{e}, "")
	if err = c.InitAdmin(ctx, "acceptance", "disposable-acceptance-password"); err != nil {
		return err
	}
	target, _ := url.Parse("http://" + addr)
	mux := http.NewServeMux()
	mux.Handle("/ws", httputil.NewSingleHostReverseProxy(target))
	mux.Handle("/", c.Handler())
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	c.PublicURL = srv.URL
	c.Start(ctx)
	defer c.Close()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err = os.WriteFile(filepath.Join(dir, "ca.pem"), cert, 0600); err != nil {
		return err
	}
	browser := srv.Client()
	browser.Jar, _ = cookiejar.New(nil)
	csrf := ""
	request := func(method, path string, value any) (map[string]any, error) {
		b, _ := json.Marshal(value)
		req, _ := http.NewRequest(method, srv.URL+"/api/v1"+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		res, err := browser.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 300 {
			return nil, fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, body)
		}
		var out map[string]any
		err = json.Unmarshal(body, &out)
		return out, err
	}
	login, err := request("POST", "/auth/login", map[string]string{"username": "acceptance", "password": "disposable-acceptance-password"})
	if err != nil {
		return err
	}
	csrf = login["csrf"].(string)
	en, err := request("POST", "/enrollments", map[string]string{"name": "installed-service"})
	if err != nil {
		return err
	}
	if err = write("info.json", map[string]any{"server": "wss" + strings.TrimPrefix(srv.URL, "https"), "token": en["token"]}); err != nil {
		return err
	}
	var clientID string
	deadline := time.Now().Add(5 * time.Minute)
	for clientID == "" {
		if time.Now().After(deadline) {
			return fmt.Errorf("service enrollment timeout")
		}
		st, _ := repo.Read(ctx)
		for _, client := range st.Clients {
			clientID = client.ID
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer tcp.Close()
	go func() {
		for {
			conn, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); io.Copy(conn, conn) }()
		}
	}()
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 65535)
		for {
			n, a, err := udp.ReadFrom(b)
			if err != nil {
				return
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	visitors := []map[string]any{}
	for _, spec := range []struct {
		protocol string
		addr     net.Addr
	}{{"tcp", tcp.Addr()}, {"udp", udp.LocalAddr()}} {
		host, port, _ := net.SplitHostPort(spec.addr.String())
		var pn int
		json.Unmarshal([]byte(port), &pn)
		op, err := request("POST", "/clients/"+clientID+"/tunnels", map[string]any{"protocol": spec.protocol, "target_ip": host, "target_port": pn})
		if err != nil {
			return err
		}
		tid := op["resource_id"].(string)
		cred, err := request("POST", "/tunnels/"+tid+"/access-credentials", map[string]any{})
		if err != nil {
			return err
		}
		secret, err := request("POST", "/access-credentials/"+cred["resource_id"].(string)+"/reveal", map[string]any{})
		if err != nil {
			return err
		}
		u, _ := url.Parse(srv.URL)
		q := url.Values{"path": {"/ws"}, "tunnel.id": {tid}, "secure": {"true"}, "caFile": {filepath.Join(dir, "ca.pem")}}
		visitors = append(visitors, map[string]any{"protocol": spec.protocol, "forward": "tunnel+wss://" + secret["username"].(string) + ":" + secret["secret"].(string) + "@" + u.Host + "?" + q.Encode()})
	}
	for {
		n := 0
		for _, s := range e.Snapshot().Sessions {
			if s.Kind == "provider" && s.Connected {
				n++
			}
		}
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("provider bind timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err = write("ready.json", map[string]any{"client_id": clientID, "visitors": visitors}); err != nil {
		return err
	}
	// The runner owns this process and terminates it after lifecycle checks.
	<-ctx.Done()
	return nil
}
