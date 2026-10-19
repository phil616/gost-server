package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

type Edge interface {
	Apply(context.Context, model.Policy) error
	Snapshot(context.Context) (model.RuntimeSnapshot, error)
}
type Server struct {
	Repo         store.Repository
	Vault        *secure.Vault
	Edge         Edge
	PublicURL    string
	WebDir       string
	CookieSecure bool
	mu           sync.Mutex
	peers        map[string]*peer
	edgeSnapshot model.RuntimeSnapshot
	edgeError    string
	cancel       context.CancelFunc
	Now          func() time.Time
	loginLimiter *rate.Limiter
}

func New(repo store.Repository, vault *secure.Vault, edge Edge, publicURL string) *Server {
	return &Server{Repo: repo, Vault: vault, Edge: edge, PublicURL: publicURL, CookieSecure: true, peers: map[string]*peer{}, Now: time.Now, loginLimiter: rate.NewLimiter(rate.Every(2*time.Second), 10)}
}
func (s *Server) audit(st *model.State, actor, action, resource string) {
	a := model.Audit{ID: uuid.NewString(), Actor: actor, Action: action, Resource: resource, At: s.Now()}
	st.Audits[a.ID] = a
}
func (s *Server) InitAdmin(ctx context.Context, user, password string) error {
	if len(user) == 0 || len(password) < 12 {
		return errors.New("username required; password must contain at least 12 characters")
	}
	hash := secure.Password(password)
	return s.Repo.Update(ctx, func(st *model.State) error {
		for _, a := range st.Admins {
			if a.Username == user {
				return errors.New("administrator already exists")
			}
		}
		a := model.Admin{ID: uuid.NewString(), Username: user, PasswordHash: hash}
		st.Admins[a.ID] = a
		if st.Meta.PolicyRevision == 0 {
			st.Meta.PolicyRevision = 1
			p := s.policy(st)
			st.Policies["1"] = p
		}
		return nil
	})
}
func (s *Server) credential(st *model.State, t model.Tunnel, kind string, expires *time.Time) (model.Credential, error) {
	id := uuid.NewString()
	secret := secure.Token()
	c := model.Credential{ID: id, TunnelID: t.ID, ClientID: t.ClientID, Kind: kind, Username: kind[:1] + "_" + id, Digest: model.Digest([]byte(secret)), Ciphertext: s.Vault.Seal(id, []byte(secret)), Generation: t.Generation, ExpiresAt: expires, CreatedAt: s.Now()}
	st.Credentials[c.ID] = c
	return c, nil
}
func (s *Server) provider(st *model.State, t model.Tunnel) error {
	for id, c := range st.Credentials {
		if c.TunnelID == t.ID && c.Kind == "provider" && !c.Revoked {
			c.Revoked = true
			st.Credentials[id] = c
		}
	}
	if !t.Enabled || t.Deleted {
		return nil
	}
	_, e := s.credential(st, t, "provider", nil)
	return e
}
func (s *Server) config(st *model.State, id string) (model.ClientConfig, error) {
	client, ok := st.Clients[id]
	if !ok {
		return model.ClientConfig{}, errors.New("unknown client")
	}
	c := model.ClientConfig{SchemaVersion: 1, ClientID: id, Revision: client.Revision, Tunnels: []model.TunnelSpec{}}
	for _, t := range st.Tunnels {
		if t.ClientID != id || t.Deleted {
			continue
		}
		spec := model.TunnelSpec{Tunnel: t}
		if t.Enabled && !client.Revoked {
			for _, cred := range st.Credentials {
				if cred.TunnelID == t.ID && cred.Kind == "provider" && !cred.Revoked && cred.Generation == t.Generation {
					b, e := s.Vault.Open(cred.ID, cred.Ciphertext)
					if e != nil {
						return c, e
					}
					spec.Provider = &model.Secret{ID: cred.ID, Username: cred.Username, Secret: string(b)}
					break
				}
			}
		}
		c.Tunnels = append(c.Tunnels, spec)
	}
	sort.Slice(c.Tunnels, func(i, j int) bool { return c.Tunnels[i].ID < c.Tunnels[j].ID })
	c.Hash = c.HashValue()
	return c, nil
}
func (s *Server) policy(st *model.State) model.Policy {
	p := model.Policy{Revision: st.Meta.PolicyRevision, Tunnels: []model.Tunnel{}, Credentials: []model.Credential{}}
	active := map[string]bool{}
	for _, t := range st.Tunnels {
		if c, ok := st.Clients[t.ClientID]; !ok || c.Revoked {
			t.Enabled = false
		}
		if !t.Enabled || t.Deleted {
			continue
		}
		active[t.ID] = true
		p.Tunnels = append(p.Tunnels, t)
	}
	for _, c := range st.Credentials {
		if c.Revoked || !active[c.TunnelID] {
			continue
		}
		c.Ciphertext = ""
		p.Credentials = append(p.Credentials, c)
	}
	sort.Slice(p.Tunnels, func(i, j int) bool { return p.Tunnels[i].ID < p.Tunnels[j].ID })
	sort.Slice(p.Credentials, func(i, j int) bool { return p.Credentials[i].ID < p.Credentials[j].ID })
	p.Hash = p.HashValue()
	return p
}
func (s *Server) queue(st *model.State, resource string, clientIDs ...string) (model.Operation, error) {
	st.Meta.PolicyRevision++
	for _, id := range clientIDs {
		c, ok := st.Clients[id]
		if !ok {
			return model.Operation{}, errors.New("client not found")
		}
		c.Revision++
		st.Clients[id] = c
		cfg, e := s.config(st, id)
		if e != nil {
			return model.Operation{}, e
		}
		b, e := json.Marshal(cfg)
		if e != nil {
			return model.Operation{}, e
		}
		key := fmt.Sprintf("%s:%d", id, c.Revision)
		st.Configs[key] = model.ConfigRecord{ID: key, ClientID: id, Revision: c.Revision, Hash: cfg.Hash, Ciphertext: s.Vault.Seal(key, b), CreatedAt: s.Now()}
	}
	p := s.policy(st)
	st.Policies[fmt.Sprint(p.Revision)] = p
	op := model.Operation{ID: uuid.NewString(), Resource: resource, PolicyRevision: p.Revision, State: "pending", CreatedAt: s.Now()}
	st.Operations[op.ID] = op
	st.Outbox[op.ID] = op
	return op, nil
}
func (s *Server) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			if pruner, ok := s.Repo.(interface {
				Prune(context.Context, time.Time) error
			}); ok {
				if e := pruner.Prune(ctx, s.Now()); e != nil && ctx.Err() == nil {
					s.LogError(e)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if e := s.reconcile(ctx); e != nil && ctx.Err() == nil {
				s.mu.Lock()
				s.edgeError = e.Error()
				s.mu.Unlock()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *Server) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.peers {
		p.close()
	}
}
func (s *Server) reconcile(ctx context.Context) error {
	st, e := s.Repo.Read(ctx)
	if e != nil {
		return e
	}
	if st.Meta.PolicyRevision < 1 {
		return nil
	}
	s.mu.Lock()
	known := s.edgeSnapshot.Revision
	knownHash := s.edgeSnapshot.PolicyHash
	s.mu.Unlock()
	desired := s.policy(&st)
	if known != desired.Revision || knownHash != desired.Hash {
		if e = s.Edge.Apply(ctx, desired); e != nil {
			return e
		}
	}
	snap, e := s.Edge.Snapshot(ctx)
	if e != nil {
		return e
	}
	s.mu.Lock()
	s.edgeSnapshot = snap
	s.edgeError = ""
	peers := make(map[string]*peer, len(s.peers))
	for k, p := range s.peers {
		peers[k] = p
	}
	s.mu.Unlock()
	if snap.Revision != desired.Revision || snap.PolicyHash != desired.Hash {
		return errors.New("edge policy acknowledgement does not match desired revision/hash")
	}
	needsSave := false
	for _, op := range st.Outbox {
		if op.State != "applied" && op.PolicyRevision <= snap.Revision {
			needsSave = true
		}
	}
	if needsSave {
		e = s.Repo.Update(ctx, func(current *model.State) error {
			for id, op := range current.Outbox {
				if op.PolicyRevision <= snap.Revision && op.State != "applied" {
					op.State = "applied"
					op.Error = ""
					current.Outbox[id] = op
					current.Operations[id] = op
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	for id, p := range peers {
		client, ok := st.Clients[id]
		if !ok || client.Revoked {
			p.close()
			continue
		}
		p.mu.Lock()
		revision := p.sent
		last := p.lastSent
		p.mu.Unlock()
		if client.Revision != revision || client.AppliedRevision < client.Revision && s.Now().Sub(last) > 30*time.Second {
			cfg, e := s.config(&st, id)
			if e != nil {
				return e
			}
			if e = p.send("desired_config", cfg); e != nil {
				p.close()
				continue
			}
			p.mu.Lock()
			p.sent = client.Revision
			p.lastSent = s.Now()
			p.mu.Unlock()
		}
	}
	return nil
}
func (s *Server) LogError(e error) { slog.Error("control operation failed", "error", e) }
