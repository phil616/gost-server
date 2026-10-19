package edge

import (
	"context"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/go-gost/relay"
	"github.com/google/uuid"
	"net"
	"testing"
	"time"
)

func testPolicy() model.Policy {
	id := uuid.NewString()
	p := model.Policy{Revision: 1, Tunnels: []model.Tunnel{{ID: id, ClientID: "client", Protocol: "tcp", TargetIP: "127.0.0.1", TargetPort: 22, Enabled: true, Generation: 1}}, Credentials: []model.Credential{{ID: "provider", TunnelID: id, ClientID: "client", Username: "provider", Kind: "provider", Generation: 1, Digest: model.Digest([]byte("secret"))}, {ID: "visitor", TunnelID: id, ClientID: "client", Username: "visitor", Kind: "visitor", Digest: model.Digest([]byte("secret"))}}}
	p.Hash = p.HashValue()
	return p
}
func TestRoleScopeGenerationAndLiveRevocation(t *testing.T) {
	e, _ := New("")
	p := testPolicy()
	if err := e.Apply(p); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		user         string
		cmd          uint8
		tid, network string
	}{{"visitor", uint8(relay.CmdBind), p.Tunnels[0].ID, "tcp"}, {"provider", uint8(relay.CmdConnect), p.Tunnels[0].ID, "tcp"}, {"visitor", uint8(relay.CmdConnect), uuid.NewString(), "tcp"}, {"visitor", uint8(relay.CmdConnect), p.Tunnels[0].ID, "udp"}}
	for _, tc := range tests {
		a, b := net.Pipe()
		if _, err := e.Admit(context.Background(), tc.user, "secret", tc.cmd, tc.network, tc.tid, a); err == nil {
			t.Fatal("unauthorized admission", tc)
		}
		a.Close()
		b.Close()
	}
	a, b := net.Pipe()
	defer b.Close()
	conn, err := e.Admit(context.Background(), "visitor", "secret", uint8(relay.CmdConnect), "tcp", p.Tunnels[0].ID, a)
	if err != nil {
		t.Fatal(err)
	}
	e.Connected(conn)
	if len(e.Snapshot().Sessions) != 1 {
		t.Fatal("session not indexed")
	}
	p.Revision++
	p.Credentials[1].Revoked = true
	p.Hash = p.HashValue()
	if err = e.Apply(p); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot().Sessions) != 0 {
		t.Fatal("revoked session remains")
	}
	b.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = b.Read(make([]byte, 1)); err == nil {
		t.Fatal("revocation did not close live transport")
	}
	if e.Check(conn) == nil {
		t.Fatal("revoked admission passed recheck")
	}
}
func TestPolicySnapshotRestartsAndHashConflicts(t *testing.T) {
	path := t.TempDir() + "/policy.json"
	e, _ := New(path)
	p := testPolicy()
	if err := e.Apply(p); err != nil {
		t.Fatal(err)
	}
	restored, err := New(path)
	if err != nil || restored.Policy().Revision != 1 {
		t.Fatal("restore", err)
	}
	changed := p
	changed.Tunnels[0].TargetPort = 23
	changed.Hash = changed.HashValue()
	if restored.Apply(changed) == nil {
		t.Fatal("same revision accepted different hash")
	}
}
