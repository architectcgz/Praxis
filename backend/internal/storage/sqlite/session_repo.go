package sqlite

import (
	"context"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsession "praxis/internal/core/domain/session"
	"time"
)

func (s *Store) GetSession(ctx context.Context, id domainfoundation.SessionID) (domainsession.Session, error) {
	var value domainsession.Session
	return value, s.loadPayload(ctx, "sessions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]domainsession.Session, error) {
	return listTargetPayloads[domainsession.Session](
		ctx,
		s,
		`SELECT payload FROM sessions ORDER BY updated_at DESC, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"sessions",
		func(value domainsession.Session) error { return value.Validate() },
	)
}

func (s *Store) ListSessionsByProject(
	ctx context.Context,
	projectID domainfoundation.ProjectID,
	limit int,
) ([]domainsession.Session, error) {
	return listTargetPayloads[domainsession.Session](
		ctx,
		s,
		`SELECT payload FROM sessions WHERE project_id = ? ORDER BY updated_at DESC, id LIMIT ?`,
		[]any{projectID.String(), targetLimit(limit)},
		"project sessions",
		func(value domainsession.Session) error { return value.Validate() },
	)
}

func (s *Store) SaveSession(ctx context.Context, value domainsession.Session) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO sessions (
			id, project_id, workspace_id, state, created_at, updated_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET project_id = excluded.project_id,
		 workspace_id = excluded.workspace_id, state = excluded.state,
		 created_at = excluded.created_at, updated_at = excluded.updated_at,
		 payload = excluded.payload`,
		value.ID.String(), value.ProjectID.String(), value.WorkspaceID.String(), string(value.State),
		value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano), payload,
	)
}

type SessionRepository struct{ store *Store }

func (r SessionRepository) Get(ctx context.Context, id domainfoundation.SessionID) (domainsession.Session, error) {
	return r.store.GetSession(ctx, id)
}

func (r SessionRepository) Save(ctx context.Context, value domainsession.Session) error {
	return r.store.SaveSession(ctx, value)
}

func (r SessionRepository) List(ctx context.Context, limit int) ([]domainsession.Session, error) {
	return r.store.ListSessions(ctx, limit)
}

func (r SessionRepository) ListByProject(
	ctx context.Context,
	projectID domainfoundation.ProjectID,
	limit int,
) ([]domainsession.Session, error) {
	return r.store.ListSessionsByProject(ctx, projectID, limit)
}
