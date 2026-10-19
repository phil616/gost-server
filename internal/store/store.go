package store

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"

	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository interface {
	Read(context.Context) (model.State, error)
	Update(context.Context, func(*model.State) error) error
}
type Postgres struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Postgres, error) {
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	return &Postgres{p}, nil
}
func (p *Postgres) Close() { p.Pool.Close() }
func (p *Postgres) Migrate(ctx context.Context) error {
	db := stdlib.OpenDBFromPool(p.Pool)
	defer db.Close()
	provider, e := goose.NewProvider(goose.DialectPostgres, db, mustSub())
	if e != nil {
		return e
	}
	_, e = provider.Up(ctx)
	return e
}
func mustSub() fs.FS {
	sub, e := fs.Sub(migrations, "migrations")
	if e != nil {
		panic(e)
	}
	return sub
}

type table struct {
	name   string
	target any
}

func tables(s *model.State) []table {
	return []table{{"admins", &s.Admins}, {"admin_sessions", &s.Sessions}, {"clients", &s.Clients}, {"enrollments", &s.Enrollments}, {"tunnels", &s.Tunnels}, {"credentials", &s.Credentials}, {"config_revisions", &s.Configs}, {"edge_policy_revisions", &s.Policies}, {"operations", &s.Operations}, {"outbox_events", &s.Outbox}, {"audit_events", &s.Audits}}
}
func read(ctx context.Context, tx pgx.Tx) (model.State, error) {
	s := model.NewState()
	meta, err := db.New(tx).GetControlMeta(ctx)
	if err != nil {
		return s, err
	}
	if e := json.Unmarshal(meta, &s.Meta); e != nil {
		return s, e
	}
	for _, t := range tables(&s) {
		query := "SELECT id,payload FROM " + t.name
		if t.name == "config_revisions" {
			query = "SELECT DISTINCT ON (client_id) id,payload FROM config_revisions ORDER BY client_id,revision DESC"
		}
		if t.name == "edge_policy_revisions" {
			query = "SELECT id,payload FROM edge_policy_revisions WHERE id=(SELECT payload->>'policy_revision' FROM control_meta WHERE id=1)"
		}
		rows, e := tx.Query(ctx, query)
		if e != nil {
			return s, e
		}
		data := map[string]json.RawMessage{}
		for rows.Next() {
			var id string
			var b []byte
			if e = rows.Scan(&id, &b); e != nil {
				rows.Close()
				return s, e
			}
			data[id] = b
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return s, e
		}
		b, _ := json.Marshal(data)
		if e = json.Unmarshal(b, t.target); e != nil {
			return s, e
		}
	}
	return s, nil
}
func (p *Postgres) Read(ctx context.Context) (model.State, error) {
	tx, e := p.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return model.State{}, e
	}
	defer tx.Rollback(ctx)
	s, e := read(ctx, tx)
	if e != nil {
		return s, e
	}
	return s, tx.Commit(ctx)
}
func docs(v any) map[string]json.RawMessage {
	b, _ := json.Marshal(v)
	var out map[string]json.RawMessage
	_ = json.Unmarshal(b, &out)
	return out
}
func (p *Postgres) Update(ctx context.Context, fn func(*model.State) error) error {
	tx, e := p.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(72634001)"); e != nil {
		return e
	}
	s, e := read(ctx, tx)
	if e != nil {
		return e
	}
	old := map[string]map[string]json.RawMessage{}
	for _, t := range tables(&s) {
		old[t.name] = docs(t.target)
	}
	if e = fn(&s); e != nil {
		return e
	}
	b, e := json.Marshal(s.Meta)
	if e != nil {
		return e
	}
	if e = db.New(tx).SetControlMeta(ctx, b); e != nil {
		return e
	}
	for _, t := range tables(&s) {
		next := docs(t.target)
		for id, b := range next {
			if bytes.Equal(old[t.name][id], b) {
				continue
			}
			if _, e = tx.Exec(ctx, "INSERT INTO "+t.name+"(id,payload) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET payload=EXCLUDED.payload", id, []byte(b)); e != nil {
				return fmt.Errorf("save %s: %w", t.name, e)
			}
		}
		for id := range old[t.name] {
			if _, ok := next[id]; !ok {
				if _, e = tx.Exec(ctx, "DELETE FROM "+t.name+" WHERE id=$1", id); e != nil {
					return e
				}
			}
		}
	}
	return tx.Commit(ctx)
}

// Memory is a transactionally isolated test repository; production commands require PostgreSQL.
type Memory struct {
	mu    sync.Mutex
	state model.State
}

func NewMemory() *Memory { return &Memory{state: model.NewState()} }
func clone(s model.State) model.State {
	b, _ := json.Marshal(s)
	v := model.NewState()
	_ = json.Unmarshal(b, &v)
	return v
}
func (m *Memory) Read(ctx context.Context) (model.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(m.state), ctx.Err()
}
func (m *Memory) Update(ctx context.Context, fn func(*model.State) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	s := clone(m.state)
	if e := fn(&s); e != nil {
		return e
	}
	m.state = s
	return nil
}
