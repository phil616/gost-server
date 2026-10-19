package control

import (
	"context"
	"errors"
	"sort"

	"github.com/altasci/altascitunnel/internal/model"
)

// RecoverRevisions prepares a restored database before accepting traffic. Every
// visitor is revoked because a backup may predate a security revocation. Device
// identities remain stable while providers and monotonically increasing desired
// revisions are renewed. Run with control/edge stopped and a floor greater than
// every revision issued since the backup.
func (s *Server) RecoverRevisions(ctx context.Context, floor int64) error {
	if floor < 1 || floor >= 9007199254740991 {
		return errors.New("revision floor must be a positive JSON-safe integer below 9007199254740991")
	}
	return s.Repo.Update(ctx, func(st *model.State) error {
		if floor <= st.Meta.PolicyRevision {
			return errors.New("revision floor must exceed the database policy revision")
		}
		st.Meta.PolicyRevision = floor
		ids := make([]string, 0, len(st.Clients))
		for id, c := range st.Clients {
			if floor <= c.Revision {
				return errors.New("revision floor must exceed every database client revision")
			}
			c.Revision = floor
			st.Clients[id] = c
			ids = append(ids, id)
		}
		for id, credential := range st.Credentials {
			credential.Revoked = true
			st.Credentials[id] = credential
		}
		for id, tunnel := range st.Tunnels {
			tunnel.Generation++
			tunnel.Version++
			st.Tunnels[id] = tunnel
			if !st.Clients[tunnel.ClientID].Revoked {
				if err := s.provider(st, tunnel); err != nil {
					return err
				}
			}
		}
		sort.Strings(ids)
		if _, err := s.queue(st, "database-recovery", ids...); err != nil {
			return err
		}
		s.audit(st, "local-admin", "database.recover", "database-recovery")
		return nil
	})
}
