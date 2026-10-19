package store

import (
	"context"
	"time"
)

func (p *Postgres) Prune(ctx context.Context, now time.Time) error {
	tx, e := p.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(72634001)"); e != nil {
		return e
	}
	for _, statement := range []struct {
		sql    string
		cutoff time.Time
	}{
		{`DELETE FROM admin_sessions WHERE (payload->>'expires_at')::timestamptz < $1`, now},
		{`DELETE FROM audit_events WHERE (payload->>'at')::timestamptz < $1`, now.Add(-180 * 24 * time.Hour)},
		{`DELETE FROM runtime_snapshots WHERE (payload->>'updated_at')::timestamptz < $1`, now.Add(-7 * 24 * time.Hour)},
		{`DELETE FROM config_revisions c WHERE (c.payload->>'created_at')::timestamptz < $1 AND c.revision < (SELECT max(n.revision) FROM config_revisions n WHERE n.client_id=c.client_id)`, now.Add(-7 * 24 * time.Hour)},
		{`DELETE FROM edge_policy_revisions p WHERE p.id<>(SELECT payload->>'policy_revision' FROM control_meta WHERE id=1) AND EXISTS(SELECT 1 FROM operations o WHERE o.payload->>'policy_revision'=p.id AND (o.payload->>'created_at')::timestamptz < $1)`, now.Add(-7 * 24 * time.Hour)},
		{`DELETE FROM outbox_events WHERE payload->>'state'='applied' AND (payload->>'created_at')::timestamptz < $1`, now.Add(-180 * 24 * time.Hour)},
		{`DELETE FROM operations WHERE payload->>'state'='applied' AND (payload->>'created_at')::timestamptz < $1`, now.Add(-180 * 24 * time.Hour)},
	} {
		if _, e = tx.Exec(ctx, statement.sql, statement.cutoff); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
