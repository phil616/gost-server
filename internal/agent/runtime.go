package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/altasci/altascitunnel/internal/gostutil"
	"github.com/altasci/altascitunnel/internal/localfile"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/retry"
	"github.com/go-gost/core/chain"
	"github.com/go-gost/core/handler"
	"github.com/go-gost/core/listener"
	"github.com/go-gost/core/service"
	xchain "github.com/go-gost/x/chain"
	"github.com/go-gost/x/config"
	chainparser "github.com/go-gost/x/config/parsing/chain"
	_ "github.com/go-gost/x/connector/tunnel"
	_ "github.com/go-gost/x/dialer/ws"
	"github.com/go-gost/x/handler/forward/remote"
	"github.com/go-gost/x/hop"
	"github.com/go-gost/x/listener/rtcp"
	"github.com/go-gost/x/listener/rudp"
	"github.com/go-gost/x/logger"
	"github.com/go-gost/x/metadata"
	xservice "github.com/go-gost/x/service"
)

type ApplyResult struct {
	Received int64             `json:"received_revision"`
	Applied  int64             `json:"applied_revision"`
	Errors   map[string]string `json:"errors"`
}
type Runtime struct {
	mu        sync.Mutex
	applyMu   sync.Mutex
	persistMu sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	endpoint  string
	caFile    string
	directory string
	jobs      map[string]*job
	statuses  map[string]model.TunnelStatus
	retries   map[string]retry.State
	received  int64
	applied   int64
	events    chan model.TunnelStatus
}
type job struct {
	spec   model.TunnelSpec
	cancel context.CancelFunc
	svc    service.Service
	policy *retry.Policy
	in     atomic.Uint64
	out    atomic.Uint64
}

func NewRuntime(ctx context.Context, endpoint, caFile, directory string) *Runtime {
	ctx, cancel := context.WithCancel(ctx)
	r := &Runtime{ctx: ctx, cancel: cancel, endpoint: endpoint, caFile: caFile, directory: directory, jobs: map[string]*job{}, statuses: map[string]model.TunnelStatus{}, retries: map[string]retry.State{}, events: make(chan model.TunnelStatus, 128)}
	if directory != "" {
		if b, e := os.ReadFile(filepath.Join(directory, "retries.json")); e == nil {
			_ = json.Unmarshal(b, &r.retries)
		}
	}
	return r
}
func (r *Runtime) Events() <-chan model.TunnelStatus { return r.events }
func (r *Runtime) publish(spec model.TunnelSpec, state string, err error, p *retry.Policy) {
	s := model.TunnelStatus{TunnelID: spec.ID, Generation: spec.Generation, State: state, UpdatedAt: time.Now()}
	if err != nil {
		s.Error = err.Error()
	}
	if p != nil {
		rs := p.Snapshot()
		s.NextRetry = rs.Next
		s.RetryCount = rs.Count
	}
	r.mu.Lock()
	if j := r.jobs[spec.ID]; j != nil && j.spec.Generation == spec.Generation {
		s.InputBytes = j.in.Load()
		s.OutputBytes = j.out.Load()
		r.statuses[spec.ID] = s
	}
	r.mu.Unlock()
	select {
	case r.events <- s:
	default:
	}
}
func (r *Runtime) Snapshot() []model.TunnelStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := make([]model.TunnelStatus, 0, len(r.statuses))
	for id, v := range r.statuses {
		if j := r.jobs[id]; j != nil {
			v.InputBytes = j.in.Load()
			v.OutputBytes = j.out.Load()
		}
		s = append(s, v)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].TunnelID < s[j].TunnelID })
	return s
}
func (r *Runtime) Revisions() (int64, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.received, r.applied
}
func (r *Runtime) saveRetry(id string, s retry.State) {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.retries[id] = s
	if r.directory != "" {
		_ = localfile.Write(filepath.Join(r.directory, "retries.json"), r.retries)
	}
}
func (r *Runtime) Apply(ctx context.Context, c model.ClientConfig) (ApplyResult, error) {
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	res := ApplyResult{Received: c.Revision, Errors: map[string]string{}}
	if e := c.Validate(); e != nil {
		return res, e
	}
	r.mu.Lock()
	if c.Revision < r.received {
		res.Received = r.received
		res.Applied = r.applied
		r.mu.Unlock()
		return res, nil
	}
	r.received = c.Revision
	r.mu.Unlock()
	desired := map[string]model.TunnelSpec{}
	for _, spec := range c.Tunnels {
		if _, ok := desired[spec.ID]; ok {
			return res, errors.New("duplicate TunnelID")
		}
		if e := spec.Validate(); e != nil {
			return res, e
		}
		desired[spec.ID] = spec
	}
	r.mu.Lock()
	var stop []*job
	for id, j := range r.jobs {
		spec, ok := desired[id]
		if !ok || !sameRuntime(spec, j.spec) {
			stop = append(stop, j)
			delete(r.jobs, id)
			delete(r.statuses, id)
		}
	}
	r.mu.Unlock()
	for _, j := range stop {
		j.cancel()
		if j.svc != nil {
			_ = j.svc.Close()
		}
	}
	for _, spec := range c.Tunnels {
		if !spec.Enabled || spec.Deleted {
			r.mu.Lock()
			r.statuses[spec.ID] = model.TunnelStatus{TunnelID: spec.ID, Generation: spec.Generation, State: "disabled", UpdatedAt: time.Now()}
			r.mu.Unlock()
			continue
		}
		r.mu.Lock()
		_, exists := r.jobs[spec.ID]
		r.mu.Unlock()
		if exists {
			continue
		}
		if spec.Provider == nil {
			res.Errors[spec.ID] = "missing Provider credential"
			continue
		}
		if e := r.start(spec); e != nil {
			res.Errors[spec.ID] = e.Error()
			r.mu.Lock()
			r.statuses[spec.ID] = model.TunnelStatus{TunnelID: spec.ID, Generation: spec.Generation, State: "error", Error: e.Error(), UpdatedAt: time.Now()}
			r.mu.Unlock()
		}
	}
	r.mu.Lock()
	for id := range r.statuses {
		if _, ok := desired[id]; !ok {
			delete(r.statuses, id)
		}
	}
	if len(res.Errors) == 0 {
		r.applied = c.Revision
	}
	res.Applied = r.applied
	r.mu.Unlock()
	return res, ctx.Err()
}

func sameRuntime(a, b model.TunnelSpec) bool {
	return a.ID == b.ID && a.Generation == b.Generation && a.Protocol == b.Protocol && a.TargetIP == b.TargetIP && a.TargetPort == b.TargetPort && a.Enabled == b.Enabled && a.Deleted == b.Deleted && reflect.DeepEqual(a.Provider, b.Provider)
}
func (r *Runtime) Close() error {
	r.cancel()
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	r.mu.Lock()
	jobs := r.jobs
	r.jobs = map[string]*job{}
	r.mu.Unlock()
	for _, j := range jobs {
		j.cancel()
		if j.svc != nil {
			_ = j.svc.Close()
		}
	}
	return nil
}
func (r *Runtime) start(spec model.TunnelSpec) error {
	u, e := url.Parse(r.endpoint)
	if e != nil {
		return e
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return errors.New("GOST endpoint must be ws/wss")
	}
	// GOST's chain router dials a host:port address; unlike net/http it does
	// not infer the WebSocket scheme's default port from a bare hostname.
	if u.Port() == "" {
		port := "443"
		if u.Scheme == "ws" {
			port = "80"
		}
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.persistMu.Lock()
	saved := r.retries[spec.ID]
	r.persistMu.Unlock()
	p := retry.New(saved)
	p.Save = func(s retry.State) { r.saveRetry(spec.ID, s) }
	j := &job{spec: spec, cancel: cancel, policy: p}
	log := logger.Nop()
	ch, e := chainparser.ParseChain(&config.ChainConfig{Name: spec.ID, Hops: []*config.HopConfig{{Name: spec.ID, Nodes: []*config.NodeConfig{{Name: spec.ID, Addr: u.Host, Connector: &config.ConnectorConfig{Type: "tunnel", Auth: &config.AuthConfig{Username: spec.Provider.Username, Password: spec.Provider.Secret}, Metadata: map[string]any{"tunnel.id": spec.ID, "connectTimeout": "15s", "mux.keepaliveInterval": "15s", "mux.keepaliveTimeout": "45s"}}, Dialer: &config.DialerConfig{Type: u.Scheme, TLS: &config.TLSConfig{Secure: true, ServerName: u.Hostname(), CAFile: r.caFile}, Metadata: map[string]any{"path": u.Path, "ws.keepalive": true, "keepalive.interval": "15s", "handshakeTimeout": "15s"}}}}}}}, log)
	if e != nil {
		cancel()
		return e
	}
	base := xchain.NewRouter(chain.ChainRouterOption(ch), chain.TimeoutRouterOption(15*time.Second), chain.RetriesRouterOption(0), chain.LoggerRouterOption(log))
	observed := &bindRouter{Router: base, parent: ctx, policy: p, connected: func() { r.publish(spec, "connected", nil, p) }, disconnected: func(err error) { r.publish(spec, "connecting", err, p) }}
	var ln listener.Listener
	opts := []listener.Option{listener.AddrOption(":0"), listener.RouterOption(observed), listener.LoggerOption(log)}
	if spec.Protocol == "tcp" {
		ln = rtcp.NewListener(opts...)
	} else {
		ln = rudp.NewListener(opts...)
	}
	if e = ln.Init(metadata.NewMetadata(map[string]any{"ttl": "60s", "readBufferSize": 65535})); e != nil {
		cancel()
		return e
	}
	target := net.JoinHostPort(spec.TargetIP, strconv.Itoa(spec.TargetPort))
	fixed := &targetRouter{Router: xchain.NewRouter(chain.LoggerRouterOption(log)), target: target, protocol: spec.Protocol, parent: ctx, in: &j.in, out: &j.out}
	h := remote.NewHandler(handler.RouterOption(fixed), handler.LoggerOption(log))
	if e = h.Init(metadata.NewMetadata(map[string]any{"sniffing": false})); e != nil {
		ln.Close()
		cancel()
		return e
	}
	h.(handler.Forwarder).Forward(hop.NewHop(hop.NodeOption(chain.NewNode(spec.ID, target)), hop.LoggerOption(log)))
	j.svc = xservice.NewService(spec.ID, ln, h, xservice.LoggerOption(log), xservice.RetryOption(ctx, func(ctx context.Context, err error) error {
		p.Failed()
		r.publish(spec, "backoff", err, p)
		if e := p.Await(ctx); e != nil {
			return e
		}
		r.publish(spec, "connecting", nil, p)
		return nil
	}))
	r.mu.Lock()
	r.jobs[spec.ID] = j
	r.mu.Unlock()
	r.publish(spec, "connecting", nil, p)
	go func() {
		if e := p.Await(ctx); e != nil {
			return
		}
		if e := j.svc.Serve(); e != nil && ctx.Err() == nil {
			r.publish(spec, "error", e, p)
		}
	}()
	return nil
}

type bindRouter struct {
	chain.Router
	parent       context.Context
	policy       *retry.Policy
	connected    func()
	disconnected func(error)
}

func (r *bindRouter) Bind(_ context.Context, network, address string, opts ...chain.BindOption) (net.Listener, error) {
	ctx, cancel := context.WithTimeout(r.parent, 15*time.Second)
	defer cancel()
	ln, e := r.Router.Bind(ctx, network, address, opts...)
	if e != nil {
		return nil, e
	}
	r.policy.Reset()
	r.connected()
	return &observedListener{Listener: ln, onError: r.disconnected}, nil
}

type observedListener struct {
	net.Listener
	onError func(error)
	once    sync.Once
}

func (l *observedListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		l.once.Do(func() { l.onError(e) })
	}
	return c, e
}

type targetRouter struct {
	chain.Router
	target, protocol string
	parent           context.Context
	in, out          *atomic.Uint64
}

func (r *targetRouter) Dial(_ context.Context, network, _ string, _ ...chain.DialOption) (net.Conn, error) {
	if network != r.protocol && network != r.protocol+"4" {
		return nil, fmt.Errorf("network mismatch")
	}
	ctx, cancel := context.WithTimeout(r.parent, 15*time.Second)
	defer cancel()
	c, e := r.Router.Dial(ctx, r.protocol, r.target)
	if e != nil {
		return nil, e
	}
	stop := context.AfterFunc(r.parent, func() { c.Close() })
	return &countConn{Conn: c, in: r.in, out: r.out, stop: stop}, nil
}

type countConn struct {
	net.Conn
	in, out *atomic.Uint64
	stop    func() bool
}

func (c *countConn) Read(b []byte) (int, error) {
	n, e := c.Conn.Read(b)
	c.in.Add(uint64(n))
	return n, e
}
func (c *countConn) Write(b []byte) (int, error) {
	n, e := c.Conn.Write(b)
	c.out.Add(uint64(n))
	return n, e
}
func (c *countConn) Close() error { c.stop(); return c.Conn.Close() }
