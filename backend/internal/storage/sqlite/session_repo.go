package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetSession(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	var value domain.Session
	return value, s.loadPayload(ctx, "sessions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]domain.Session, error) {
	return listTargetPayloads[domain.Session](
		ctx,
		s,
		`SELECT payload FROM sessions ORDER BY rowid DESC LIMIT ?`,
		[]any{targetLimit(limit)},
		"sessions",
		func(value domain.Session) error { return value.Validate() },
	)
}

func (s *Store) SaveSession(ctx context.Context, value domain.Session) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO sessions (id, workspace_key, payload) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET workspace_key = excluded.workspace_key, payload = excluded.payload`,
		value.ID.String(), value.WorkspaceKey, payload,
	)
}

type SessionRepository struct{ store *Store }

func (r SessionRepository) Get(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	return r.store.GetSession(ctx, id)
}

func (r SessionRepository) Save(ctx context.Context, value domain.Session) error {
	return r.store.SaveSession(ctx, value)
}

func (r SessionRepository) List(ctx context.Context, limit int) ([]domain.Session, error) {
	return r.store.ListSessions(ctx, limit)
}
