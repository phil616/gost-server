package integration

import (
	"context"
	"encoding/json"
	"github.com/altasci/altascitunnel/internal/agent"
	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/google/uuid"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	a := l.Addr().String()
	l.Close()
	return a
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout")
}
func native(t *testing.T, args ...string) {
	t.Helper()
	binary := os.Getenv("GOST_BINARY")
	if binary == "" {
		t.Skip("set GOST_BINARY to official gost v3.3.0")
	}
	v, e := exec.Command(binary, "-V").CombinedOutput()
	if e != nil || !strings.Contains(string(v), "gost v3.3.0 ") {
		t.Fatalf("official v3.3.0 required: %s %v", v, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	if e = cmd.Start(); e != nil {
		cancel()
		t.Fatal(e)
	}
	t.Cleanup(func() { cancel(); cmd.Wait() })
}
func TestNativeGostTCPUDPAndFixedTarget(t *testing.T) {
	if os.Getenv("GOST_BINARY") == "" {
		t.Skip("set GOST_BINARY")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine, e := edge.New("")
	if e != nil {
		t.Fatal(e)
	}
	addr := freeAddress(t)
	if e = engine.Start(ctx, addr); e != nil {
		t.Fatal(e)
	}
	tcp, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer tcp.Close()
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 65535)
		for {
			n, a, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	p := model.Policy{Revision: 1, Tunnels: []model.Tunnel{}, Credentials: []model.Credential{}}
	cfg := model.ClientConfig{SchemaVersion: 1, ClientID: uuid.NewString(), Revision: 1, Tunnels: []model.TunnelSpec{}}
	for _, tc := range []struct {
		network string
		origin  net.Addr
	}{{"tcp", tcp.Addr()}, {"udp", udp.LocalAddr()}} {
		host, port, _ := net.SplitHostPort(tc.origin.String())
		var n int
		json.Unmarshal([]byte(port), &n)
		tid := uuid.NewString()
		tunnel := model.Tunnel{ID: tid, ClientID: cfg.ClientID, Protocol: tc.network, TargetIP: host, TargetPort: n, Generation: 1, Enabled: true}
		p.Tunnels = append(p.Tunnels, tunnel)
		p.Credentials = append(p.Credentials, model.Credential{ID: uuid.NewString(), ClientID: cfg.ClientID, TunnelID: tid, Username: "provider-" + tid, Kind: "provider", Generation: 1, Digest: model.Digest([]byte("provider-secret"))}, model.Credential{ID: uuid.NewString(), ClientID: cfg.ClientID, TunnelID: tid, Username: "visitor-" + tid, Kind: "visitor", Digest: model.Digest([]byte("visitor-secret"))})
		cfg.Tunnels = append(cfg.Tunnels, model.TunnelSpec{Tunnel: tunnel, Provider: &model.Secret{Username: "provider-" + tid, Secret: "provider-secret"}})
	}
	p.Hash = p.HashValue()
	if e = engine.Apply(p); e != nil {
		t.Fatal(e)
	}
	r := agent.NewRuntime(ctx, "ws://"+addr+"/ws", "", t.TempDir())
	defer r.Close()
	cfg.Hash = cfg.HashValue()
	if res, e := r.Apply(ctx, cfg); e != nil || len(res.Errors) > 0 {
		t.Fatal(res, e)
	}
	waitFor(t, func() bool {
		n := 0
		for _, s := range engine.Snapshot().Sessions {
			if s.Kind == "provider" && s.Connected {
				n++
			}
		}
		return n == 2
	})
	for _, spec := range cfg.Tunnels {
		t.Run(spec.Protocol, func(t *testing.T) {
			local := freeAddress(t)
			listen := spec.Protocol + "://" + local + "/203.0.113.9:65000"
			if spec.Protocol == "udp" {
				listen += "?keepalive=true&ttl=60s"
			}
			native(t, "-L", listen, "-F", "tunnel+ws://visitor-"+spec.ID+":visitor-secret@"+addr+"?path=/ws&tunnel.id="+spec.ID)
			var c net.Conn
			waitFor(t, func() bool {
				var err error
				c, err = net.DialTimeout(spec.Protocol, local, 200*time.Millisecond)
				return err == nil
			})
			defer c.Close()
			time.Sleep(200 * time.Millisecond)
			payload := []byte("native-gost-fixed-origin")
			c.SetDeadline(time.Now().Add(5 * time.Second))
			if _, e = c.Write(payload); e != nil {
				t.Fatal(e)
			}
			b := make([]byte, len(payload))
			if _, e = io.ReadFull(c, b); e != nil || string(b) != string(payload) {
				t.Fatalf("round trip: %q %v", b, e)
			}
			if spec.Protocol == "tcp" {
				p.Revision++
				for i := range p.Credentials {
					if p.Credentials[i].TunnelID == spec.ID && p.Credentials[i].Kind == "visitor" {
						p.Credentials[i].Revoked = true
					}
				}
				p.Hash = p.HashValue()
				if e = engine.Apply(p); e != nil {
					t.Fatal(e)
				}
				c.SetReadDeadline(time.Now().Add(3 * time.Second))
				if _, e = c.Read(make([]byte, 1)); e == nil {
					t.Fatal("live Visitor session not revoked")
				}
			}
		})
	}
}
