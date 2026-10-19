package control

import (
	"bytes"
	"context"
	"testing"

	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
)

type corruptAckEdge struct{ testEdge }

func (e corruptAckEdge) Snapshot(ctx context.Context) (model.RuntimeSnapshot, error) {
	s, err := e.testEdge.Snapshot(ctx)
	s.PolicyHash = "different-policy-with-the-same-revision"
	return s, err
}

func TestPolicyHashMismatchNeverAcknowledgesOperation(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemory()
	vault, _ := secure.New(bytes.Repeat([]byte{9}, 32))
	engine, _ := edge.New("")
	s := New(repo, vault, corruptAckEdge{testEdge{engine}}, "https://test.invalid")
	if err := s.InitAdmin(ctx, "admin", "test-password-long"); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := repo.Update(ctx, func(st *model.State) error {
		op, err := s.queue(st, "test-resource")
		id = op.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("accepted an acknowledgement for a different policy")
	}
	st, _ := repo.Read(ctx)
	if st.Operations[id].State != "pending" {
		t.Fatal("incorrectly reported operation as applied")
	}
}
