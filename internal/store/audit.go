package store

import (
	"context"
	"datascope-server/internal/audit"
)

func (s *Store) validateAudit(ctx context.Context, query string, raw bool) error {
	var err error
	if raw {
		_, err = NormalizeRawReadOnlySQL(query)
	} else {
		_, err = NormalizeReadOnlySQL(query)
	}
	if err == nil {
		return nil
	}
	ev, e := s.Audit.Start(ctx, "sql.validation", query)
	if e != nil {
		return e
	}
	return audit.Join(err, s.Audit.Finish(ev, err))
}
