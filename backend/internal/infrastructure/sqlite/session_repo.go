package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
	domainsession "praxis/internal/domain/session"
)

func (s *Store) GetSession(ctx context.Context, id domainfoundation.SessionID) (domainsession.Session, error) {
	row := s.Executor(ctx).QueryRowContext(ctx, `SELECT id, project_id, workspace_id, goal, state, created_at, updated_at FROM sessions WHERE id = ?`, id.String())
	var value domainsession.Session
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.WorkspaceID, &value.Goal, &value.State, &createdAt, &updatedAt); err == sql.ErrNoRows {
		return value, domainfoundation.ErrNotFound
	} else if err != nil {
		return value, fmt.Errorf("read session: %w", err)
	}
	var err error
	value.CreatedAt, err = parseTimestamp(createdAt, "session.createdAt")
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseTimestamp(updatedAt, "session.updatedAt")
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}

func (s *Store) listSessions(ctx context.Context, query string, args ...any) ([]domainsession.Session, error) {
	rows, err := s.Executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	values := make([]domainsession.Session, 0)
	for rows.Next() {
		var value domainsession.Session
		var createdAt, updatedAt string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.WorkspaceID, &value.Goal, &value.State, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		value.CreatedAt, err = parseTimestamp(createdAt, "session.createdAt")
		if err != nil {
			return nil, err
		}
		value.UpdatedAt, err = parseTimestamp(updatedAt, "session.updatedAt")
		if err != nil {
			return nil, err
		}
		if err := value.Validate(); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]domainsession.Session, error) {
	return s.listSessions(ctx, `SELECT id, project_id, workspace_id, goal, state, created_at, updated_at FROM sessions ORDER BY updated_at DESC, id LIMIT ?`, targetLimit(limit))
}
func (s *Store) ListSessionsByProject(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainsession.Session, error) {
	return s.listSessions(ctx, `SELECT id, project_id, workspace_id, goal, state, created_at, updated_at FROM sessions WHERE project_id = ? ORDER BY updated_at DESC, id LIMIT ?`, projectID.String(), targetLimit(limit))
}
func (s *Store) SaveSession(ctx context.Context, value domainsession.Session) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.execMutation(ctx, `INSERT INTO sessions (id, project_id, workspace_id, goal, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET project_id = excluded.project_id, workspace_id = excluded.workspace_id, goal = excluded.goal, state = excluded.state, created_at = excluded.created_at, updated_at = excluded.updated_at`, value.ID.String(), value.ProjectID.String(), value.WorkspaceID.String(), value.Goal, string(value.State), value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano))
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
func (r SessionRepository) ListByProject(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainsession.Session, error) {
	return r.store.ListSessionsByProject(ctx, projectID, limit)
}
