package integration

import (
	"context"
	"fmt"
	"github.com/altasci/altascitunnel/internal/agent"
	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/google/uuid"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Opt in: ALTASCI_STRESS=1 GOST_BINARY=... go test ./internal/integration -run TestScale -timeout 5m -v
// For a release soak, also set ALTASCI_SOAK_SECONDS=86400 and -timeout 25h.
func TestScale100Clients1000Tunnels(t *testing.T) {
	if os.Getenv("ALTASCI_STRESS") != "1" || os.Getenv("GOST_BINARY") == "" {
		t.Skip("set ALTASCI_STRESS=1 and GOST_BINARY")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	origin, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer origin.Close()
	go func() {
		for {
			c, e := origin.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	host, port, _ := net.SplitHostPort(origin.Addr().String())
	targetPort, _ := strconv.Atoi(port)
	engine, _ := edge.New("")
	addr := freeAddress(t)
	if e = engine.Start(ctx, addr); e != nil {
		t.Fatal(e)
	}
	p := model.Policy{Revision: 1, Tunnels: []model.Tunnel{}, Credentials: []model.Credential{}}
	configs := make([]model.ClientConfig, 100)
	for i := range configs {
		c := model.ClientConfig{SchemaVersion: 1, ClientID: uuid.NewString(), Revision: 1}
		for j := 0; j < 10; j++ {
			tid := uuid.NewString()
			tunnel := model.Tunnel{ID: tid, ClientID: c.ClientID, Protocol: "tcp", TargetIP: host, TargetPort: targetPort, Generation: 1, Enabled: true}
			user := "provider-" + tid
			p.Tunnels = append(p.Tunnels, tunnel)
			p.Credentials = append(p.Credentials, model.Credential{ID: uuid.NewString(), ClientID: c.ClientID, TunnelID: tid, Kind: "provider", Generation: 1, Username: user, Digest: model.Digest([]byte("scale-secret"))})
			c.Tunnels = append(c.Tunnels, model.TunnelSpec{Tunnel: tunnel, Provider: &model.Secret{Username: user, Secret: "scale-secret"}})
		}
		c.Hash = c.HashValue()
		configs[i] = c
	}
	tid := configs[0].Tunnels[0].ID
	p.Credentials = append(p.Credentials, model.Credential{ID: uuid.NewString(), TunnelID: tid, ClientID: configs[0].ClientID, Kind: "visitor", Username: "scale-visitor", Digest: model.Digest([]byte("scale-access"))})
	p.Hash = p.HashValue()
	if e = engine.Apply(p); e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	runtimes := make([]*agent.Runtime, 0, 100)
	defer func() {
		for _, r := range runtimes {
			r.Close()
		}
	}()
	for _, cfg := range configs {
		r := agent.NewRuntime(ctx, "ws://"+addr+"/ws", "", "")
		runtimes = append(runtimes, r)
		res, e := r.Apply(ctx, cfg)
		if e != nil || len(res.Errors) > 0 {
			t.Fatal(e, res)
		}
	}
	deadline := time.Now().Add(120 * time.Second)
	connected := 0
	for time.Now().Before(deadline) {
		connected = 0
		for _, s := range engine.Snapshot().Sessions {
			if s.Kind == "provider" && s.Connected {
				connected++
			}
		}
		if connected == 1000 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if connected != 1000 {
		t.Fatalf("only %d/1000 reverse tunnels connected", connected)
	}
	local := freeAddress(t)
	native(t, "-L", "tcp://"+local+"/arbitrary-target:1234", "-F", "tunnel+ws://scale-visitor:scale-access@"+addr+"?path=/ws&tunnel.id="+tid)
	var connections []net.Conn
	defer func() {
		for _, c := range connections {
			c.Close()
		}
	}()
	for i := 0; i < 100; i++ {
		var c net.Conn
		waitFor(t, func() bool { var err error; c, err = net.DialTimeout("tcp", local, time.Second); return err == nil })
		connections = append(connections, c)
		c.SetDeadline(time.Now().Add(15 * time.Second))
		payload := fmt.Sprintf("scale-%d", i)
		if _, e = c.Write([]byte(payload)); e != nil {
			t.Fatal(e)
		}
		b := make([]byte, len(payload))
		if _, e = io.ReadFull(c, b); e != nil || string(b) != payload {
			t.Fatal("payload mismatch", e)
		}
	}
	active := 0
	for _, s := range engine.Snapshot().Sessions {
		if s.Kind == "visitor" {
			active++
		}
	}
	if active != 100 {
		t.Fatalf("%d active Visitors, want 100", active)
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	t.Logf("100 Clients, 1000 tunnels, 100 live TCP connections; startup=%v Go heap=%dMiB goroutines=%d", time.Since(started).Round(time.Millisecond), mem.Alloc/(1024*1024), runtime.NumGoroutine())
	seconds, _ := strconv.Atoi(os.Getenv("ALTASCI_SOAK_SECONDS"))
	if seconds < 1 {
		seconds = 2
	}
	end := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(end) {
		for i, c := range connections {
			c.SetDeadline(time.Now().Add(10 * time.Second))
			payload := fmt.Sprintf("soak-%d", i)
			if _, e = c.Write([]byte(payload)); e != nil {
				t.Fatal(e)
			}
			b := make([]byte, len(payload))
			if _, e = io.ReadFull(c, b); e != nil || string(b) != payload {
				t.Fatal(e)
			}
		}
		time.Sleep(time.Second)
	}
}
