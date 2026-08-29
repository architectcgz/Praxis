package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetTaskSession(ctx context.Context, id domain.TaskSessionID) (domain.TaskSession, error) {
	var value domain.TaskSession
	return value, s.loadPayload(ctx, "task_sessions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveTaskSession(ctx context.Context, value domain.TaskSession) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO task_sessions (id, workspace_key, state, payload) VALUES (?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        workspace_key = excluded.workspace_key,
	        state = excluded.state,
	        payload = excluded.payload`,
		value.ID.String(),
		value.WorkspaceKey,
		string(value.State),
		payload,
	)
}

type TaskSessionRepository struct{ store *Store }

func (r TaskSessionRepository) Get(ctx context.Context, id domain.TaskSessionID) (domain.TaskSession, error) {
	return r.store.GetTaskSession(ctx, id)
}

func (r TaskSessionRepository) Save(ctx context.Context, value domain.TaskSession) error {
	return r.store.SaveTaskSession(ctx, value)
}
