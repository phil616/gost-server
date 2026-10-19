package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/altasci/altascitunnel/internal/gostutil"
	"github.com/altasci/altascitunnel/internal/localfile"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/go-gost/core/handler"
	"github.com/go-gost/core/listener"
	"github.com/go-gost/core/service"
	"github.com/go-gost/relay"
	tunnel "github.com/go-gost/x/handler/tunnel"
	ws "github.com/go-gost/x/listener/ws"
	"github.com/go-gost/x/logger"
	"github.com/go-gost/x/metadata"
	xservice "github.com/go-gost/x/service"
	"github.com/google/uuid"
)

var ErrUnauthorized = errors.New("unauthorized tunnel operation")

type Engine struct {
	mu           sync.Mutex
	policy       model.Policy
	credentials  map[string]model.Credential
	tunnels      map[string]model.Tunnel
	sessions     map[string]*trackedConn
	epoch        string
	sequence     uint64
	authRejected atomic.Uint64
	inputBytes   atomic.Uint64
	outputBytes  atomic.Uint64
	path         string
	svc          service.Service
}
type trackedConn struct {
	net.Conn
	engine   *Engine
	info     model.SessionInfo
	username string
	digest   string
	read     atomic.Uint64
	written  atomic.Uint64
	closed   atomic.Bool
}

func (c *trackedConn) Read(b []byte) (int, error) {
	n, e := c.Conn.Read(b)
	c.read.Add(uint64(n))
	c.engine.inputBytes.Add(uint64(n))
	return n, e
}
func (c *trackedConn) Write(b []byte) (int, error) {
	n, e := c.Conn.Write(b)
	c.written.Add(uint64(n))
	c.engine.outputBytes.Add(uint64(n))
	return n, e
}
func (c *trackedConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	e := c.Conn.Close()
	c.engine.mu.Lock()
	delete(c.engine.sessions, c.info.ID)
	c.engine.sequence++
	c.engine.mu.Unlock()
	return e
}
func New(path string) (*Engine, error) {
	e := &Engine{path: path, epoch: uuid.NewString(), credentials: map[string]model.Credential{}, tunnels: map[string]model.Tunnel{}, sessions: map[string]*trackedConn{}}
	if path != "" {
		b, err := os.ReadFile(path)
		if err == nil {
			var p model.Policy
			if err = json.Unmarshal(b, &p); err != nil || p.Hash != p.HashValue() {
				slog.Error("invalid policy snapshot; waiting for control synchronization")
			} else {
				e.install(p)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return e, nil
}
func (e *Engine) install(p model.Policy) {
	e.policy = p
	e.credentials = map[string]model.Credential{}
	e.tunnels = map[string]model.Tunnel{}
	for _, c := range p.Credentials {
		e.credentials[c.Username] = c
	}
	for _, t := range p.Tunnels {
		e.tunnels[t.ID] = t
	}
}
func (e *Engine) valid(c *trackedConn, now time.Time) bool {
	cred, ok := e.credentials[c.username]
	if !ok || cred.ID != c.info.CredentialID || cred.Revoked || cred.Digest != c.digest {
		return false
	}
	if cred.ExpiresAt != nil && !now.Before(*cred.ExpiresAt) {
		return false
	}
	t, ok := e.tunnels[c.info.TunnelID]
	if !ok || !t.Enabled || t.Deleted || t.Generation != c.info.Generation || cred.TunnelID != t.ID {
		return false
	}
	if c.info.Kind == "provider" {
		return cred.Kind == "provider" && cred.Generation == t.Generation && cred.ClientID == t.ClientID
	}
	return cred.Kind == "visitor"
}
func (e *Engine) Apply(p model.Policy) error {
	if p.Revision < 1 || p.Hash != p.HashValue() {
		return errors.New("invalid policy revision/hash")
	}
	for _, t := range p.Tunnels {
		if err := t.Validate(); err != nil {
			return err
		}
	}
	e.mu.Lock()
	if p.Revision < e.policy.Revision {
		e.mu.Unlock()
		return errors.New("stale policy revision")
	}
	if p.Revision == e.policy.Revision {
		same := p.Hash == e.policy.Hash
		e.mu.Unlock()
		if !same {
			return errors.New("revision/hash conflict")
		}
		return nil
	}
	if e.path != "" {
		if err := localfile.Write(e.path, p); err != nil {
			e.mu.Unlock()
			return err
		}
	}
	e.install(p)
	var closeList []*trackedConn
	for _, c := range e.sessions {
		if !e.valid(c, time.Now()) {
			c.closed.Store(true)
			delete(e.sessions, c.info.ID)
			closeList = append(closeList, c)
		}
	}
	e.sequence++
	e.mu.Unlock()
	for _, c := range closeList {
		_ = c.Conn.Close()
	}
	return nil
}
func (e *Engine) Admit(_ context.Context, user, pass string, cmd uint8, network, tid string, conn net.Conn) (net.Conn, error) {
	e.mu.Lock()
	cred, ok := e.credentials[user]
	t, tok := e.tunnels[tid]
	kind := ""
	if cmd == uint8(relay.CmdBind) {
		kind = "provider"
	} else if cmd == uint8(relay.CmdConnect) {
		kind = "visitor"
	}
	c := &trackedConn{Conn: conn, engine: e, username: user, digest: cred.Digest, info: model.SessionInfo{ID: uuid.NewString(), CredentialID: cred.ID, TunnelID: tid, Generation: t.Generation, Kind: kind, OpenedAt: time.Now()}}
	if !ok || !tok || kind == "" || cred.Kind != kind || network != t.Protocol || !secure.EqualDigest(pass, cred.Digest) || !e.valid(c, time.Now()) {
		e.authRejected.Add(1)
		e.mu.Unlock()
		return conn, ErrUnauthorized
	}
	var old []*trackedConn
	if kind == "provider" {
		for id, active := range e.sessions {
			if active.info.Kind == "provider" && active.info.TunnelID == tid {
				active.closed.Store(true)
				delete(e.sessions, id)
				old = append(old, active)
			}
		}
	}
	e.sessions[c.info.ID] = c
	e.sequence++
	e.mu.Unlock()
	for _, v := range old {
		_ = v.Conn.Close()
	}
	return c, nil
}
func (e *Engine) Check(conn net.Conn) error {
	c, ok := conn.(*trackedConn)
	if !ok {
		return ErrUnauthorized
	}
	e.mu.Lock()
	valid := !c.closed.Load() && e.valid(c, time.Now())
	e.mu.Unlock()
	if !valid {
		_ = c.Close()
		return ErrUnauthorized
	}
	return nil
}
func (e *Engine) Connected(conn net.Conn) {
	c, ok := conn.(*trackedConn)
	if !ok {
		return
	}
	e.mu.Lock()
	if !c.closed.Load() && e.valid(c, time.Now()) {
		c.info.Connected = true
		e.sequence++
	}
	e.mu.Unlock()
}
func (e *Engine) Snapshot() model.RuntimeSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := model.RuntimeSnapshot{Epoch: e.epoch, Sequence: e.sequence, Revision: e.policy.Revision, PolicyHash: e.policy.Hash, UpdatedAt: time.Now(), Sessions: []model.SessionInfo{}}
	for _, c := range e.sessions {
		v := c.info
		v.InputBytes = c.read.Load()
		v.OutputBytes = c.written.Load()
		s.Sessions = append(s.Sessions, v)
	}
	sort.Slice(s.Sessions, func(i, j int) bool { return s.Sessions[i].ID < s.Sessions[j].ID })
	return s
}
func (e *Engine) Policy() model.Policy {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.policy
	p.Credentials = nil
	p.Tunnels = nil
	return p
}
func (e *Engine) Start(ctx context.Context, addr string) error {
	log := logger.Nop()
	h := tunnel.NewHandlerWithHooks(e, handler.LoggerOption(log))
	if err := h.Init(metadata.NewMetadata(map[string]any{"tunnel.direct": true, "readTimeout": 15 * time.Second, "mux.keepaliveInterval": 15 * time.Second, "mux.keepaliveTimeout": 45 * time.Second})); err != nil {
		return err
	}
	ln := ws.NewListener(listener.AddrOption(addr), listener.LoggerOption(log))
	if err := ln.Init(metadata.NewMetadata(map[string]any{"path": "/ws"})); err != nil {
		return err
	}
	e.svc = xservice.NewService("altasci-edge", ln, h, xservice.LoggerOption(log))
	go e.svc.Serve()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = e.svc.Close()
				return
			case <-ticker.C:
				e.expire()
			}
		}
	}()
	return nil
}
func (e *Engine) expire() {
	e.mu.Lock()
	var list []*trackedConn
	for id, c := range e.sessions {
		if !e.valid(c, time.Now()) {
			c.closed.Store(true)
			delete(e.sessions, id)
			list = append(list, c)
		}
	}
	if len(list) > 0 {
		e.sequence++
	}
	e.mu.Unlock()
	for _, c := range list {
		_ = c.Conn.Close()
	}
}
func (e *Engine) String() string { return fmt.Sprintf("edge %s", e.epoch) }
