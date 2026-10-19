package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type testEdge struct{ *edge.Engine }

func (e testEdge) Apply(_ context.Context, p model.Policy) error { return e.Engine.Apply(p) }

func (e testEdge) Snapshot(context.Context) (model.RuntimeSnapshot, error) {
	return e.Engine.Snapshot(), nil
}
func TestTLSRegistrationRecoveryConfigAndIdempotency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var repo store.Repository = store.NewMemory()
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		base, e := store.Open(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		defer base.Close()
		schema := "control_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, e = base.Pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
			t.Fatal(e)
		}
		defer base.Pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		u, _ := url.Parse(dsn)
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		pg, e := store.Open(ctx, u.String())
		if e != nil {
			t.Fatal(e)
		}
		defer pg.Close()
		if e = pg.Migrate(ctx); e != nil {
			t.Fatal(e)
		}
		repo = pg
	}
	vault, _ := secure.New(bytes.Repeat([]byte{9}, 32))
	eng, _ := edge.New("")
	server := New(repo, vault, testEdge{eng}, "https://example.test")
	if e := server.InitAdmin(ctx, "admin", "test-password-long"); e != nil {
		t.Fatal(e)
	}
	httpServer := httptest.NewTLSServer(server.Handler())
	defer httpServer.Close()
	defer server.Close()
	browser := httpServer.Client()
	browser.Jar, _ = cookiejar.New(nil)
	csrf := ""
	request := func(method, path, body, key string, want int) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(method, httpServer.URL+"/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Idempotency-Key", key)
		res, e := browser.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s status %d want %d: %s", method, path, res.StatusCode, want, b)
		}
		var out map[string]any
		json.Unmarshal(b, &out)
		return out
	}
	login := request("POST", "/auth/login", `{"username":"admin","password":"test-password-long"}`, "", 200)
	csrf = login["csrf"].(string)
	enrollment := request("POST", "/enrollments", `{"name":"integration-device"}`, "", 201)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	requestID := uuid.NewString()
	wsURL := "wss" + strings.TrimPrefix(httpServer.URL, "https") + "/agent/v1/ws"
	connect := func(kind, token string) (*websocket.Conn, string) {
		t.Helper()
		c, _, e := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: httpServer.Client()})
		if e != nil {
			t.Fatal(e)
		}
		var challenge model.Message
		if e = wsjson.Read(ctx, c, &challenge); e != nil {
			t.Fatal(e)
		}
		var n map[string]string
		json.Unmarshal(challenge.Payload, &n)
		a := authenticate{Token: token, PublicKey: base64.RawURLEncoding.EncodeToString(pub), RequestID: requestID, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(n["nonce"]))), OS: "linux", Arch: "amd64", Version: "test"}
		if e = writeMessage(ctx, c, kind, a); e != nil {
			t.Fatal(e)
		}
		var authenticated model.Message
		if e = wsjson.Read(ctx, c, &authenticated); e != nil {
			t.Fatal(e)
		}
		if authenticated.Type != "authenticated" {
			t.Fatalf("unexpected %s %s", authenticated.Type, authenticated.Payload)
		}
		var payload map[string]any
		json.Unmarshal(authenticated.Payload, &payload)
		return c, payload["client_id"].(string)
	}
	conn, clientID := connect("register", enrollment["token"].(string))
	if e := server.reconcile(ctx); e != nil {
		t.Fatal(e)
	}
	var msg model.Message
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	if e := wsjson.Read(readCtx, conn, &msg); e != nil {
		t.Fatal(e)
	}
	if msg.Type != "desired_config" {
		t.Fatal(msg.Type)
	}
	var cfg model.ClientConfig
	json.Unmarshal(msg.Payload, &cfg)
	if cfg.ClientID != clientID || cfg.Hash != cfg.HashValue() {
		t.Fatal("invalid config")
	}
	body := `{"name":"ssh","protocol":"tcp","target_ip":"8.8.8.8","target_port":22}`
	created := request("POST", "/clients/"+clientID+"/tunnels", body, "same-request", 202)
	again := request("POST", "/clients/"+clientID+"/tunnels", body, "same-request", 202)
	if created["resource_id"] != again["resource_id"] {
		t.Fatal("idempotency created duplicate tunnel")
	}
	request("POST", "/clients/"+clientID+"/tunnels", `{"protocol":"udp","target_ip":"8.8.8.8","target_port":53}`, "same-request", 409)
	if e := server.reconcile(ctx); e != nil {
		t.Fatal(e)
	}
	if e := wsjson.Read(readCtx, conn, &msg); e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(msg.Payload, &cfg)
	if len(cfg.Tunnels) != 1 || cfg.Tunnels[0].Provider == nil || cfg.Tunnels[0].TargetIP != "8.8.8.8" {
		t.Fatalf("incorrect desired config: %#v", cfg)
	}
	token := cfg.Tunnels[0].Provider.Secret
	if token == "" {
		t.Fatal("missing provider secret")
	}
	conn.CloseNow()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		server.mu.Lock()
		gone := server.peers[clientID] == nil
		server.mu.Unlock()
		if gone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	recovered, id := connect("register", "")
	if id != clientID {
		t.Fatal("registration recovery changed identity")
	}
	recovered.CloseNow()
	st, _ := repo.Read(ctx)
	if len(st.Clients) != 1 || len(st.Tunnels) != 1 {
		t.Fatal("duplicate persistent records")
	}
	for _, record := range st.Configs {
		if strings.Contains(record.Ciphertext, token) {
			t.Fatal("plaintext secret in config history")
		}
	}
	for _, a := range st.Audits {
		if strings.Contains(a.Resource, token) {
			t.Fatal("secret leaked to audit")
		}
	}
	csrf = "bad"
	request("POST", "/enrollments", `{"name":"blocked"}`, "", 403)
}
