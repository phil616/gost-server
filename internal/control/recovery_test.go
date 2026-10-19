package control

import (
	"bytes"
	"context"
	"testing"

	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
	"github.com/google/uuid"
)

func TestRecoveryKeepsIdentityButRevokesVisitorsAndRotatesProviders(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemory()
	vault, _ := secure.New(bytes.Repeat([]byte{5}, 32))
	engine, _ := edge.New("")
	s := New(repo, vault, testEdge{engine}, "https://test.invalid")
	clientID, tunnelID := uuid.NewString(), uuid.NewString()
	var visitorID string
	if err := repo.Update(ctx, func(st *model.State) error {
		st.Clients[clientID] = model.Client{ID: clientID, PublicKey: "unchanged-device-key", Revision: 5}
		tunnel := model.Tunnel{ID: tunnelID, ClientID: clientID, Protocol: "tcp", TargetIP: "127.0.0.1", TargetPort: 22, Enabled: true, Generation: 1, Version: 1}
		st.Tunnels[tunnelID] = tunnel
		if err := s.provider(st, tunnel); err != nil {
			return err
		}
		c, err := s.credential(st, tunnel, "visitor", nil)
		visitorID = c.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverRevisions(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.Read(ctx)
	if st.Clients[clientID].PublicKey != "unchanged-device-key" || st.Clients[clientID].Revision != 1001 || st.Meta.PolicyRevision != 1001 {
		t.Fatal("identity or revision recovery failed")
	}
	if !st.Credentials[visitorID].Revoked || st.Tunnels[tunnelID].Generation != 2 {
		t.Fatal("stale authorization survived recovery")
	}
	active := 0
	for _, c := range st.Credentials {
		if !c.Revoked {
			if c.Kind != "provider" || c.Generation != 2 {
				t.Fatal("unexpected active authorization")
			}
			active++
		}
	}
	if active != 1 {
		t.Fatal("provider rotation failed")
	}
	cfg, err := s.config(&st, clientID)
	if err != nil || cfg.Validate() != nil {
		t.Fatal("invalid recovery configuration", err)
	}
	if err := s.RecoverRevisions(ctx, 10); err == nil {
		t.Fatal("accepted an older recovery revision")
	}
	current, _ := repo.Read(ctx)
	if current.Meta.PolicyRevision != 1001 {
		t.Fatal("failed recovery was not rolled back")
	}
}
