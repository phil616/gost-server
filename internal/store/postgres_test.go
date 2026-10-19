package store

import (
	"context"
	"fmt"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/google/uuid"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestPostgresMigrationsTransactionsAndConstraints(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL test instance")
	}
	ctx := context.Background()
	base, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer base.Close()
	schema := "altasci_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = base.Pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer base.Pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = db.Migrate(ctx); e != nil {
		t.Fatal("migration not idempotent", e)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- db.Update(ctx, func(st *model.State) error {
				id := fmt.Sprint(i)
				st.Admins[id] = model.Admin{ID: id, Username: "admin-" + id, PasswordHash: "test"}
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Fatal(e)
		}
	}
	s, e := db.Read(ctx)
	if e != nil || len(s.Admins) != 4 {
		t.Fatal("lost transaction", e, len(s.Admins))
	}
	e = db.Update(ctx, func(st *model.State) error {
		st.Admins["rollback"] = model.Admin{ID: "rollback", Username: "rollback"}
		return fmt.Errorf("abort")
	})
	if e == nil {
		t.Fatal("expected rollback")
	}
	s, _ = db.Read(ctx)
	if _, ok := s.Admins["rollback"]; ok {
		t.Fatal("aborted transaction committed")
	}
	e = db.Update(ctx, func(st *model.State) error {
		st.Tunnels["invalid"] = model.Tunnel{ID: "invalid", ClientID: "missing", Protocol: "tcp", TargetIP: "127.0.0.1", TargetPort: 22}
		return nil
	})
	if e == nil {
		t.Fatal("foreign key not enforced")
	}
}
