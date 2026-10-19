package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
)

func TestLoginUnknownUsernameAndPasswordVerification(t *testing.T) {
	repo := store.NewMemory()
	vault, err := secure.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	server := New(repo, vault, nil, "")
	defer server.Close()
	const password = "test-login-password"
	if err := server.InitAdmin(context.Background(), "admin", password); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	for _, tc := range []struct {
		name, username, password string
	}{
		{"known user wrong password", "admin", "wrong-password"},
		{"unknown user wrong password", "unknown", "wrong-password"},
		{"unknown user with real admin password", "unknown", password},
		{"empty username", "", password},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"username": tc.username, "password": tc.password})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
			response := httptest.NewRecorder()
			start := time.Now()
			handler.ServeHTTP(response, request)
			t.Logf("failed login verification took %s", time.Since(start))
			if response.Code != http.StatusUnauthorized || response.Body.String() != "{\"error\":\"invalid credentials\"}\n" {
				t.Fatalf("unexpected failed login response: %d %s", response.Code, response.Body.String())
			}
			if len(response.Result().Cookies()) != 0 {
				t.Fatal("failed login set a session cookie")
			}
			state, err := repo.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Sessions) != 0 {
				t.Fatal("failed login created a session")
			}
		})
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid login failed: %d %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "altasci_session" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie: %v", cookies)
	}
	me := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	me.AddCookie(cookies[0])
	authenticated := httptest.NewRecorder()
	handler.ServeHTTP(authenticated, me)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("new session cannot authenticate: %d %s", authenticated.Code, authenticated.Body.String())
	}
}
