package control

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestBrowserOriginProtection(t *testing.T) {
	repo := store.NewMemory()
	vault, err := secure.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := New(repo, vault, nil, "https://console.test:8443")
	defer s.Close()
	if err := repo.Update(context.Background(), func(st *model.State) error {
		st.Sessions[model.Digest([]byte("session"))] = model.AdminSession{AdminID: "admin", CSRF: "csrf", ExpiresAt: time.Now().Add(time.Hour)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	for _, origin := range []string{"https://attacker.test", "http://console.test:8443", "https://console.test", "null", "https://console.test:8443/path", "https://user@console.test:8443", "https://console.test:8443?x"} {
		for _, path := range []string{"/auth/login", "/enrollments"} {
			r := httptest.NewRequest(http.MethodPost, "https://console.test:8443/api/v1"+path, strings.NewReader(`{}`))
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", "csrf")
			r.AddCookie(&http.Cookie{Name: "altasci_session", Value: "session"})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
				t.Fatalf("%s with Origin %q: %d %s", path, origin, w.Code, w.Body.String())
			}
		}
	}
	for _, tc := range []struct {
		secure bool
		origin string
	}{{true, "https://console.test:8443"}, {true, ""}, {false, "http://console.test:8443"}} {
		s.CookieSecure = tc.secure
		r := httptest.NewRequest(http.MethodPost, "https://console.test:8443/api/v1/enrollments", strings.NewReader(`{}`))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-CSRF-Token", "csrf")
		r.AddCookie(&http.Cookie{Name: "altasci_session", Value: "session"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("valid origin %q: %d %s", tc.origin, w.Code, w.Body.String())
		}
		if w.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing clickjacking protection")
		}
	}
}

func TestPendingDeviceAuthenticationLimit(t *testing.T) {
	s := New(store.NewMemory(), nil, nil, "")
	defer s.Close()
	s.agentAuthSlots = make(chan struct{}, 1)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(ts.URL, "http") + "/agent/v1/ws"
	first, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.CloseNow()
	var challenge model.Message
	if err := wsjson.Read(ctx, first, &challenge); err != nil {
		t.Fatal(err)
	}
	second, response, err := websocket.Dial(ctx, endpoint, nil)
	if second != nil {
		second.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("pending authentication was not limited: %v %v", response, err)
	}
	first.CloseNow()
	deadline := time.Now().Add(time.Second)
	for len(s.agentAuthSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	third, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		t.Fatalf("failed authentication did not release capacity: %v", err)
	}
	third.CloseNow()
}
