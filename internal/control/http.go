package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type userKey struct{}
type httpError struct {
	status  int
	message string
}

func (e httpError) Error() string          { return e.message }
func bad(status int, message string) error { return httpError{status, message} }
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) fail(w http.ResponseWriter, e error) {
	var h httpError
	if errors.As(e, &h) {
		reply(w, h.status, map[string]string{"error": h.message})
		return
	}
	s.LogError(e)
	reply(w, 503, map[string]string{"error": "operation unavailable"})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad(400, "invalid JSON: "+e.Error())
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return bad(400, "one JSON object required")
	}
	return nil
}
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "same-origin")
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.Repo.Read(r.Context()); e != nil {
			s.fail(w, e)
			return
		}
		reply(w, 200, map[string]string{"status": "ok"})
	})
	r.Get("/agent/v1/ws", s.agentWS)
	r.With(middleware.Throttle(4)).Post("/api/v1/auth/login", s.login)
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.Get("/api/v1/auth/me", s.me)
		r.Post("/api/v1/auth/logout", s.logout)
		r.Post("/api/v1/enrollments", s.createEnrollment)
		r.Delete("/api/v1/enrollments/{id}", s.revokeEnrollment)
		r.Get("/api/v1/clients", s.clients)
		r.Get("/api/v1/clients/{id}", s.client)
		r.Post("/api/v1/clients/{id}/revoke", s.revokeClient)
		r.Post("/api/v1/clients/{id}/tunnels", s.createTunnel)
		r.Get("/api/v1/tunnels", s.tunnels)
		r.Get("/api/v1/tunnels/{id}", s.tunnel)
		r.Patch("/api/v1/tunnels/{id}", s.editTunnel)
		r.Delete("/api/v1/tunnels/{id}", s.deleteTunnel)
		r.Get("/api/v1/tunnels/{id}/access-credentials", s.credentials)
		r.Post("/api/v1/tunnels/{id}/access-credentials", s.createCredential)
		r.Post("/api/v1/access-credentials/{id}/reveal", s.revealCredential)
		r.Delete("/api/v1/access-credentials/{id}", s.deleteCredential)
		r.Get("/api/v1/operations/{id}", s.operation)
		r.Get("/api/v1/audit-events", s.audits)
		r.Get("/api/v1/edge/status", s.edgeStatus)
	})
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		if s.WebDir == "" {
			http.NotFound(w, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		file := filepath.Join(s.WebDir, filepath.FromSlash(clean))
		if st, e := os.Stat(file); e == nil && !st.IsDir() {
			http.ServeFile(w, r, file)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.WebDir, "index.html"))
	})
	return r
}

// This fixed, valid Argon2id encoding is only used to give unknown usernames
// the same password verification cost as known usernames. It is not a secret.
const dummyPasswordHash = "argon2id$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// A bounded process-wide budget remains effective behind the single local
	// reverse proxy without trusting arbitrary X-Forwarded-For headers.
	if !s.loginLimiter.Allow() {
		w.Header().Set("Retry-After", "2")
		s.fail(w, bad(429, "too many login attempts"))
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &body); e != nil {
		s.fail(w, e)
		return
	}
	if len(body.Password) > 1024 {
		s.fail(w, bad(400, "password too long"))
		return
	}
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	var admin model.Admin
	for _, a := range st.Admins {
		if a.Username == body.Username {
			admin = a
			break
		}
	}
	passwordHash := admin.PasswordHash
	if admin.ID == "" {
		passwordHash = dummyPasswordHash
	}
	// Verify before checking the ID so unknown usernames also run Argon2id.
	passwordOK := secure.CheckPassword(body.Password, passwordHash)
	if admin.ID == "" || !passwordOK {
		s.fail(w, bad(401, "invalid credentials"))
		return
	}
	token := secure.Token()
	session := model.AdminSession{ID: model.Digest([]byte(token)), AdminID: admin.ID, CSRF: secure.Token(), ExpiresAt: s.Now().Add(12 * time.Hour)}
	e = s.Repo.Update(r.Context(), func(st *model.State) error {
		st.Sessions[session.ID] = session
		s.audit(st, admin.ID, "login", admin.ID)
		return nil
	})
	if e != nil {
		s.fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "altasci_session", Value: token, Path: "/", HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode, Expires: session.ExpiresAt})
	reply(w, 200, map[string]string{"username": admin.Username, "csrf": session.CSRF})
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, e := r.Cookie("altasci_session")
		if e != nil {
			s.fail(w, bad(401, "login required"))
			return
		}
		st, e := s.Repo.Read(r.Context())
		if e != nil {
			s.fail(w, e)
			return
		}
		session, ok := st.Sessions[model.Digest([]byte(cookie.Value))]
		if !ok || !s.Now().Before(session.ExpiresAt) {
			s.fail(w, bad(401, "session expired"))
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
				s.fail(w, bad(403, "invalid CSRF token"))
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || u.Host != r.Host {
					s.fail(w, bad(403, "invalid origin"))
					return
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, session)))
	})
}
func actor(r *http.Request) string { return r.Context().Value(userKey{}).(model.AdminSession).AdminID }
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(userKey{}).(model.AdminSession)
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	reply(w, 200, map[string]string{"username": st.Admins[session.AdminID].Username, "csrf": session.CSRF})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	ss := r.Context().Value(userKey{}).(model.AdminSession)
	e := s.Repo.Update(r.Context(), func(st *model.State) error { delete(st.Sessions, ss.ID); return nil })
	if e != nil {
		s.fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "altasci_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode})
	reply(w, 200, map[string]bool{"ok": true})
}
func (s *Server) createEnrollment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if e := decode(w, r, &body); e != nil {
		s.fail(w, e)
		return
	}
	token := secure.Token()
	en := model.Enrollment{ID: uuid.NewString(), Name: strings.TrimSpace(body.Name), Digest: model.Digest([]byte(token)), ExpiresAt: s.Now().Add(10 * time.Minute)}
	e := s.Repo.Update(r.Context(), func(st *model.State) error {
		st.Enrollments[en.ID] = en
		s.audit(st, actor(r), "enrollment.create", en.ID)
		return nil
	})
	if e != nil {
		s.fail(w, e)
		return
	}
	serverURL := strings.TrimRight(strings.Replace(s.PublicURL, "https://", "wss://", 1), "/")
	reply(w, 201, map[string]any{"id": en.ID, "token": token, "expires_at": en.ExpiresAt, "command": fmt.Sprintf("altascitunnel register --server %s --token %s", serverURL, token)})
}
func (s *Server) revokeEnrollment(w http.ResponseWriter, r *http.Request) {
	e := s.Repo.Update(r.Context(), func(st *model.State) error {
		id := chi.URLParam(r, "id")
		en, ok := st.Enrollments[id]
		if !ok {
			return bad(404, "enrollment not found")
		}
		en.Revoked = true
		st.Enrollments[id] = en
		s.audit(st, actor(r), "enrollment.revoke", id)
		return nil
	})
	if e != nil {
		s.fail(w, e)
		return
	}
	reply(w, 200, map[string]bool{"ok": true})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request, items any, total int) {
	reply(w, 200, map[string]any{"items": items, "total": total})
}
func page[T any](r *http.Request, in []T) ([]T, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	if p > len(in)/size+1 {
		p = len(in)/size + 1
	}
	start := (p - 1) * size
	if start > len(in) {
		start = len(in)
	}
	end := start + size
	if end > len(in) {
		end = len(in)
	}
	return in[start:end], len(in)
}
func (s *Server) clientView(c model.Client) map[string]any {
	b, _ := json.Marshal(c)
	out := map[string]any{}
	json.Unmarshal(b, &out)
	delete(out, "public_key")
	state := "offline"
	s.mu.Lock()
	p := s.peers[c.ID]
	s.mu.Unlock()
	if p != nil {
		age := s.Now().Sub(p.seen())
		if age < 45*time.Second {
			state = "online"
		} else if age < 60*time.Second {
			state = "stale"
		}
	}
	if c.Revoked {
		state = "revoked"
	}
	out["state"] = state
	return out
}
func (s *Server) clients(w http.ResponseWriter, r *http.Request) {
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	items := []model.Client{}
	for _, c := range st.Clients {
		items = append(items, c)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	part, total := page(r, items)
	views := []map[string]any{}
	for _, c := range part {
		views = append(views, s.clientView(c))
	}
	s.list(w, r, views, total)
}
func (s *Server) client(w http.ResponseWriter, r *http.Request) {
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	c, ok := st.Clients[chi.URLParam(r, "id")]
	if !ok {
		s.fail(w, bad(404, "client not found"))
		return
	}
	reply(w, 200, s.clientView(c))
}
func (s *Server) mutate(w http.ResponseWriter, r *http.Request, action string, fn func(*model.State) (string, []string, error)) {
	key := r.Header.Get("Idempotency-Key")
	if key != "" {
		key = actor(r) + ":" + key
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if e != nil {
		s.fail(w, bad(400, "request too large"))
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(raw)))
	hash := model.Digest(append([]byte(r.Method+" "+r.URL.Path+"\n"), raw...))
	var op model.Operation
	e = s.Repo.Update(r.Context(), func(st *model.State) error {
		if key != "" {
			for _, existing := range st.Operations {
				if existing.IdempotencyKey == key {
					if existing.RequestHash != hash {
						return bad(409, "idempotency key reused with different request")
					}
					op = existing
					return nil
				}
			}
		}
		resource, clients, e := fn(st)
		if e != nil {
			return e
		}
		op, e = s.queue(st, resource, clients...)
		if e != nil {
			return e
		}
		op.IdempotencyKey = key
		op.RequestHash = hash
		st.Operations[op.ID] = op
		st.Outbox[op.ID] = op
		s.audit(st, actor(r), action, resource)
		return nil
	})
	if e != nil {
		s.fail(w, e)
		return
	}
	reply(w, 202, map[string]any{"operation_id": op.ID, "resource_id": op.Resource, "policy_revision": op.PolicyRevision, "state": op.State})
}
func (s *Server) createTunnel(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, "tunnel.create", func(st *model.State) (string, []string, error) {
		var body struct {
			Name       string `json:"name"`
			Protocol   string `json:"protocol"`
			TargetIP   string `json:"target_ip"`
			TargetPort int    `json:"target_port"`
			Enabled    *bool  `json:"enabled"`
		}
		if e := decode(w, r, &body); e != nil {
			return "", nil, e
		}
		clientID := chi.URLParam(r, "id")
		c, ok := st.Clients[clientID]
		if !ok || c.Revoked {
			return "", nil, bad(404, "active client not found")
		}
		t := model.Tunnel{ID: uuid.NewString(), ClientID: clientID, Name: body.Name, Protocol: body.Protocol, TargetIP: body.TargetIP, TargetPort: body.TargetPort, Enabled: true, Generation: 1, Version: 1, CreatedAt: s.Now()}
		if body.Enabled != nil {
			t.Enabled = *body.Enabled
		}
		if e := t.Validate(); e != nil {
			return "", nil, bad(400, e.Error())
		}
		st.Tunnels[t.ID] = t
		if e := s.provider(st, t); e != nil {
			return "", nil, e
		}
		return t.ID, []string{clientID}, nil
	})
}
func (s *Server) editTunnel(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, "tunnel.update", func(st *model.State) (string, []string, error) {
		id := chi.URLParam(r, "id")
		t, ok := st.Tunnels[id]
		if !ok || t.Deleted {
			return "", nil, bad(404, "tunnel not found")
		}
		if r.Header.Get("If-Match") != strconv.FormatInt(t.Version, 10) {
			return "", nil, bad(409, "If-Match must contain current resource version")
		}
		var b struct {
			Name       *string `json:"name"`
			Protocol   *string `json:"protocol"`
			TargetIP   *string `json:"target_ip"`
			TargetPort *int    `json:"target_port"`
			Enabled    *bool   `json:"enabled"`
		}
		if e := decode(w, r, &b); e != nil {
			return "", nil, e
		}
		old := t
		if b.Name != nil {
			t.Name = *b.Name
		}
		if b.Protocol != nil {
			t.Protocol = *b.Protocol
		}
		if b.TargetIP != nil {
			t.TargetIP = *b.TargetIP
		}
		if b.TargetPort != nil {
			t.TargetPort = *b.TargetPort
		}
		if b.Enabled != nil {
			t.Enabled = *b.Enabled
		}
		if st.Clients[t.ClientID].Revoked {
			return "", nil, bad(409, "device revoked")
		}
		if e := t.Validate(); e != nil {
			return "", nil, bad(400, e.Error())
		}
		t.Version++
		changed := old.Protocol != t.Protocol || old.TargetIP != t.TargetIP || old.TargetPort != t.TargetPort || old.Enabled != t.Enabled
		if changed {
			t.Generation++
		}
		st.Tunnels[id] = t
		if changed {
			if e := s.provider(st, t); e != nil {
				return "", nil, e
			}
		}
		return id, []string{t.ClientID}, nil
	})
}
func (s *Server) deleteTunnel(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, "tunnel.delete", func(st *model.State) (string, []string, error) {
		id := chi.URLParam(r, "id")
		t, ok := st.Tunnels[id]
		if !ok {
			return "", nil, bad(404, "tunnel not found")
		}
		t.Deleted = true
		t.Enabled = false
		t.Version++
		t.Generation++
		st.Tunnels[id] = t
		for id, c := range st.Credentials {
			if c.TunnelID == t.ID {
				c.Revoked = true
				st.Credentials[id] = c
			}
		}
		return t.ID, []string{t.ClientID}, nil
	})
}
func (s *Server) revokeClient(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, "client.revoke", func(st *model.State) (string, []string, error) {
		id := chi.URLParam(r, "id")
		c, ok := st.Clients[id]
		if !ok {
			return "", nil, bad(404, "client not found")
		}
		c.Revoked = true
		st.Clients[id] = c
		for id, t := range st.Tunnels {
			if t.ClientID == c.ID {
				t.Enabled = false
				t.Generation++
				t.Version++
				st.Tunnels[id] = t
			}
		}
		for id, cred := range st.Credentials {
			if cred.ClientID == c.ID {
				cred.Revoked = true
				st.Credentials[id] = cred
			}
		}
		return c.ID, []string{c.ID}, nil
	})
}
func (s *Server) tunnelView(t model.Tunnel) map[string]any {
	b, _ := json.Marshal(t)
	v := map[string]any{}
	json.Unmarshal(b, &v)
	state := "pending"
	active := 0
	s.mu.Lock()
	snap := s.edgeSnapshot
	edgeError := s.edgeError
	peer := s.peers[t.ClientID]
	s.mu.Unlock()
	if edgeError != "" || s.Now().Sub(snap.UpdatedAt) > 45*time.Second {
		state = "unknown"
	} else {
		for _, c := range snap.Sessions {
			if c.TunnelID == t.ID && c.Generation == t.Generation {
				if c.Kind == "provider" && c.Connected {
					state = "connected"
				}
				if c.Kind == "visitor" {
					active++
				}
			}
		}
		if state != "connected" && peer != nil {
			for _, rs := range peer.runtime() {
				if rs.TunnelID == t.ID && rs.Generation == t.Generation {
					state = rs.State
					if state == "connected" {
						state = "connecting"
					}
					v["last_error"] = rs.Error
					v["next_retry_at"] = rs.NextRetry
				}
			}
		}
	}
	if !t.Enabled {
		state = "disabled"
	}
	v["state"] = state
	v["active_connections"] = active
	v["target_health"] = "unknown"
	return v
}
func (s *Server) tunnels(w http.ResponseWriter, r *http.Request) {
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	list := []model.Tunnel{}
	for _, t := range st.Tunnels {
		if !t.Deleted && (r.URL.Query().Get("client_id") == "" || t.ClientID == r.URL.Query().Get("client_id")) {
			list = append(list, t)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	part, total := page(r, list)
	out := []map[string]any{}
	for _, t := range part {
		out = append(out, s.tunnelView(t))
	}
	s.list(w, r, out, total)
}
func (s *Server) tunnel(w http.ResponseWriter, r *http.Request) {
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	t, ok := st.Tunnels[chi.URLParam(r, "id")]
	if !ok || t.Deleted {
		s.fail(w, bad(404, "tunnel not found"))
		return
	}
	w.Header().Set("ETag", strconv.FormatInt(t.Version, 10))
	reply(w, 200, s.tunnelView(t))
}
func (s *Server) createCredential(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, "credential.create", func(st *model.State) (string, []string, error) {
		var b struct {
			ExpiresAt *time.Time `json:"expires_at"`
		}
		if e := decode(w, r, &b); e != nil {
			return "", nil, e
		}
		t, ok := st.Tunnels[chi.URLParam(r, "id")]
		if !ok || t.Deleted || st.Clients[t.ClientID].Revoked {
			return "", nil, bad(404, "active tunnel not found")
		}
		if b.ExpiresAt != nil && !s.Now().Before(*b.ExpiresAt) {
			return "", nil, bad(400, "expiration must be in the future")
		}
		c, e := s.credential(st, t, "visitor", b.ExpiresAt)
		return c.ID, nil, e
	})
}
func (s *Server) credentials(w http.ResponseWriter, r *http.Request) {
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	list := []model.Credential{}
	for _, c := range st.Credentials {
		if c.TunnelID == chi.URLParam(r, "id") && c.Kind == "visitor" {
			c.Ciphertext = ""
			c.Digest = ""
			list = append(list, c)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	part, total := page(r, list)
	s.list(w, r, part, total)
}
func (s *Server) revealCredential(w http.ResponseWriter, r *http.Request) {
	var c model.Credential
	var t model.Tunnel
	e := s.Repo.Update(r.Context(), func(st *model.State) error {
		var ok bool
		c, ok = st.Credentials[chi.URLParam(r, "id")]
		if !ok || c.Kind != "visitor" || c.Revoked {
			return bad(404, "active Visitor credential not found")
		}
		t = st.Tunnels[c.TunnelID]
		s.audit(st, actor(r), "credential.reveal", c.ID)
		return nil
	})
	if e != nil {
		s.fail(w, e)
		return
	}
	b, e := s.Vault.Open(c.ID, c.Ciphertext)
	if e != nil {
		s.fail(w, e)
		return
	}
	u, e := url.Parse(s.PublicURL)
	if e != nil {
		s.fail(w, e)
		return
	}
	u.Scheme = "wss"
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "443")
	}
	u.Path = ""
	u.RawQuery = ""
	u.User = url.UserPassword(c.Username, string(b))
	q := url.Values{"path": {"/ws"}, "tunnel.id": {c.TunnelID}, "secure": {"true"}, "ws.keepalive": {"true"}}
	endpoint := "tunnel+" + u.String() + "?" + q.Encode()
	local := "tcp://127.0.0.1:2222/service.internal"
	if t.Protocol == "udp" {
		local = "udp://127.0.0.1:1053/service.internal:53?keepalive=true&ttl=60s"
	}
	command := fmt.Sprintf("gost -L %q -F %q", local, endpoint)
	yaml := fmt.Sprintf("services:\n  - name: visitor\n    addr: 127.0.0.1:%d\n    handler:\n      type: %s\n      chain: tunnel\n    listener:\n      type: %s\n      metadata:\n        keepalive: true\n        ttl: 60s\n    forwarder:\n      nodes:\n        - name: service\n          addr: service.internal:0\nchains:\n  - name: tunnel\n    hops:\n      - name: edge\n        nodes:\n          - name: edge\n            addr: %s\n            connector:\n              type: tunnel\n              auth:\n                username: %s\n                password: %s\n              metadata:\n                tunnel.id: %s\n            dialer:\n              type: wss\n              tls:\n                secure: true\n                serverName: %s\n              metadata:\n                path: /ws\n                ws.keepalive: true\n", map[bool]int{true: 1053, false: 2222}[t.Protocol == "udp"], t.Protocol, t.Protocol, u.Host, c.Username, string(b), t.ID, u.Hostname())
	reply(w, 200, map[string]string{"username": c.Username, "secret": string(b), "command": command, "yaml": yaml})
}
func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, "credential.revoke", func(st *model.State) (string, []string, error) {
		id := chi.URLParam(r, "id")
		c, ok := st.Credentials[id]
		if !ok || c.Kind != "visitor" {
			return "", nil, bad(404, "credential not found")
		}
		c.Revoked = true
		st.Credentials[id] = c
		return id, nil, nil
	})
}
func (s *Server) operation(w http.ResponseWriter, r *http.Request) {
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	op, ok := st.Operations[chi.URLParam(r, "id")]
	if !ok {
		s.fail(w, bad(404, "operation not found"))
		return
	}
	reply(w, 200, op)
}
func (s *Server) audits(w http.ResponseWriter, r *http.Request) {
	st, e := s.Repo.Read(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	list := []model.Audit{}
	for _, a := range st.Audits {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At.After(list[j].At) })
	part, total := page(r, list)
	s.list(w, r, part, total)
}
func (s *Server) edgeStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	snap := s.edgeSnapshot
	err := s.edgeError
	s.mu.Unlock()
	reply(w, 200, map[string]any{"runtime": snap, "error": err, "healthy": err == "" && s.Now().Sub(snap.UpdatedAt) < 45*time.Second})
}
