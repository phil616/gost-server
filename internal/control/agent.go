package control

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

type peer struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	queue    chan model.Message
	ctx      context.Context
	cancel   context.CancelFunc
	lastSeen time.Time
	lastSent time.Time
	sent     int64
	statuses []model.TunnelStatus
	once     sync.Once
}

func (p *peer) close() { p.once.Do(func() { p.cancel(); _ = p.conn.CloseNow() }) }
func (p *peer) send(kind string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	m := model.Message{V: 1, Type: kind, ID: uuid.NewString(), Payload: b}
	select {
	case p.queue <- m:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		return errors.New("management write queue full")
	}
}
func (p *peer) seen() time.Time { p.mu.Lock(); defer p.mu.Unlock(); return p.lastSeen }
func (p *peer) runtime() []model.TunnelStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]model.TunnelStatus(nil), p.statuses...)
}
func writeMessage(ctx context.Context, c *websocket.Conn, kind string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return wsjson.Write(ctx, c, model.Message{V: 1, Type: kind, ID: uuid.NewString(), Payload: b})
}

type authenticate struct {
	ClientID  string `json:"client_id"`
	RequestID string `json:"request_id"`
	Token     string `json:"token"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
}

func (s *Server) agentWS(w http.ResponseWriter, r *http.Request) {
	conn, e := websocket.Accept(w, r, nil)
	if e != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	nonce := secure.Token()
	if e = writeMessage(ctx, conn, "challenge", map[string]string{"nonce": nonce}); e != nil {
		return
	}
	authCtx, authCancel := context.WithTimeout(ctx, 30*time.Second)
	var m model.Message
	e = wsjson.Read(authCtx, conn, &m)
	authCancel()
	if e != nil {
		return
	}
	var a authenticate
	if m.V != 1 || (m.Type != "register" && m.Type != "authenticate") || json.Unmarshal(m.Payload, &a) != nil {
		_ = writeMessage(ctx, conn, "error", map[string]string{"code": "authentication_failed"})
		return
	}
	client, e := s.authenticate(ctx, m.Type, nonce, a)
	if e != nil {
		_ = writeMessage(ctx, conn, "error", map[string]string{"code": "authentication_failed"})
		return
	}
	pctx, pcancel := context.WithCancel(ctx)
	p := &peer{conn: conn, queue: make(chan model.Message, 32), ctx: pctx, cancel: pcancel, lastSeen: s.Now()}
	s.mu.Lock()
	old := s.peers[client.ID]
	if old != nil && s.Now().Sub(old.seen()) < 60*time.Second {
		s.mu.Unlock()
		_ = writeMessage(ctx, conn, "error", map[string]string{"code": "duplicate_instance"})
		return
	}
	s.peers[client.ID] = p
	s.mu.Unlock()
	if old != nil {
		old.close()
	}
	defer func() {
		p.close()
		s.mu.Lock()
		if s.peers[client.ID] == p {
			delete(s.peers, client.ID)
		}
		s.mu.Unlock()
	}()
	if e = writeMessage(ctx, conn, "authenticated", map[string]any{"client_id": client.ID, "revision": client.Revision}); e != nil {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case msg := <-p.queue:
				wc, cancel := context.WithTimeout(pctx, 10*time.Second)
				err := wsjson.Write(wc, conn, msg)
				cancel()
				if err != nil {
					p.close()
					return
				}
			case <-ticker.C:
				if s.Now().Sub(p.seen()) >= 60*time.Second {
					p.close()
					return
				}
				if p.send("heartbeat", map[string]any{"time": s.Now()}) != nil {
					p.close()
					return
				}
			}
		}
	}()
	for {
		var msg model.Message
		if e = wsjson.Read(pctx, conn, &msg); e != nil {
			return
		}
		if msg.V != 1 {
			return
		}
		p.mu.Lock()
		p.lastSeen = s.Now()
		p.mu.Unlock()
		switch msg.Type {
		case "hello", "heartbeat", "runtime_snapshot", "runtime_event", "config_ack":
			var body struct {
				Received int64                `json:"received_revision"`
				Applied  int64                `json:"applied_revision"`
				Runtime  []model.TunnelStatus `json:"runtime"`
				Errors   map[string]string    `json:"errors"`
			}
			if json.Unmarshal(msg.Payload, &body) != nil {
				return
			}
			if body.Runtime != nil {
				p.mu.Lock()
				p.statuses = body.Runtime
				p.mu.Unlock()
			}
			if msg.Type != "runtime_event" {
				if e = s.Repo.Update(pctx, func(st *model.State) error {
					c, ok := st.Clients[client.ID]
					if !ok || c.Revoked {
						return errors.New("device revoked")
					}
					c.LastSeen = s.Now()
					if body.Received > c.Revision || body.Applied > body.Received {
						return errors.New("invalid revision acknowledgement")
					}
					if body.Received > c.ReceivedRevision {
						c.ReceivedRevision = body.Received
					}
					if body.Applied > c.AppliedRevision {
						c.AppliedRevision = body.Applied
					}
					if body.Runtime != nil {
						c.Runtime = body.Runtime
					}
					st.Clients[c.ID] = c
					return nil
				}); e != nil {
					return
				}
			}
		default:
			return
		}
	}
}
func (s *Server) authenticate(ctx context.Context, kind, nonce string, a authenticate) (model.Client, error) {
	var client model.Client
	signature, e := base64.RawURLEncoding.DecodeString(a.Signature)
	if e != nil {
		return client, e
	}
	if kind == "authenticate" {
		st, e := s.Repo.Read(ctx)
		if e != nil {
			return client, e
		}
		c, ok := st.Clients[a.ClientID]
		if !ok || c.Revoked {
			return client, errors.New("unknown identity")
		}
		pub, e := base64.RawURLEncoding.DecodeString(c.PublicKey)
		if e != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, []byte(nonce), signature) {
			return client, errors.New("invalid signature")
		}
		return c, nil
	}
	pub, e := base64.RawURLEncoding.DecodeString(a.PublicKey)
	if e != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, []byte(nonce), signature) || a.RequestID == "" {
		return client, errors.New("invalid enrollment proof")
	}
	e = s.Repo.Update(ctx, func(st *model.State) error {
		for _, en := range st.Enrollments {
			if en.RequestID == a.RequestID && en.ClientID != "" {
				if en.PublicKey != a.PublicKey || en.Revoked {
					return errors.New("enrollment identity mismatch")
				}
				client = st.Clients[en.ClientID]
				if client.Revoked {
					return errors.New("device revoked")
				}
				return nil
			}
		}
		for _, c := range st.Clients {
			if c.PublicKey == a.PublicKey {
				return errors.New("public key already enrolled")
			}
		}
		for id, en := range st.Enrollments {
			if en.Revoked || en.ClientID != "" || !s.Now().Before(en.ExpiresAt) || !secure.EqualDigest(a.Token, en.Digest) {
				continue
			}
			client = model.Client{ID: uuid.NewString(), Name: en.Name, PublicKey: a.PublicKey, OS: a.OS, Arch: a.Arch, Version: a.Version, CreatedAt: s.Now(), LastSeen: s.Now()}
			st.Clients[client.ID] = client
			en.ClientID = client.ID
			en.RequestID = a.RequestID
			en.PublicKey = a.PublicKey
			st.Enrollments[id] = en
			s.audit(st, client.ID, "client.register", client.ID)
			_, err := s.queue(st, client.ID, client.ID)
			client = st.Clients[client.ID]
			return err
		}
		return errors.New("invalid or expired enrollment")
	})
	return client, e
}
