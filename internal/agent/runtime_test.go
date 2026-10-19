package agent

import (
	"context"
	"github.com/altasci/altascitunnel/internal/model"
	"testing"
)

func TestInvalidConfigurationDoesNotAdvanceOrReplace(t *testing.T) {
	r := NewRuntime(context.Background(), "wss://example.test/ws", "", t.TempDir())
	defer r.Close()
	cfg := model.ClientConfig{SchemaVersion: 1, ClientID: "test", Revision: 1, Tunnels: []model.TunnelSpec{}}
	cfg.Hash = cfg.HashValue()
	if _, e := r.Apply(context.Background(), cfg); e != nil {
		t.Fatal(e)
	}
	bad := cfg
	bad.Revision = 2
	bad.Tunnels = []model.TunnelSpec{{Tunnel: model.Tunnel{ID: "bad", ClientID: "test", Generation: 1, Protocol: "tcp", TargetIP: "0.0.0.0", TargetPort: 22, Enabled: true}}}
	bad.Hash = bad.HashValue()
	if _, e := r.Apply(context.Background(), bad); e == nil {
		t.Fatal("invalid config accepted")
	}
	received, applied := r.Revisions()
	if received != 1 || applied != 1 {
		t.Fatal("invalid config changed active revision", received, applied)
	}
}
