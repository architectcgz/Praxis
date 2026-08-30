package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"praxis/internal/core/domain"
)

func (s *Store) GetActiveByWorkspace(ctx context.Context, workspaceID domain.WorkspaceID) (domain.WorkspaceWriteLease, error) {
	row := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT payload FROM workspace_write_leases WHERE workspace_id = ? AND state = ?`,
		workspaceID.String(),
		string(domain.LeaseActive),
	)
	value, err := decodePayload[domain.WorkspaceWriteLease](
		row,
		func(value domain.WorkspaceWriteLease) error { return value.Validate() },
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceWriteLease{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkspaceWriteLease{}, fmt.Errorf("get active workspace lease: %w", err)
	}
	return value, nil
}

func (s *Store) SaveWorkspaceLease(ctx context.Context, value domain.WorkspaceWriteLease) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO workspace_write_leases (
        id, workspace_id, workspace_path_snapshot, workspace_revision, owner_agent_id,
        capability_grant_id, state, acquired_at, released_at, payload
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET workspace_id = excluded.workspace_id,
        workspace_path_snapshot = excluded.workspace_path_snapshot,
        workspace_revision = excluded.workspace_revision, owner_agent_id = excluded.owner_agent_id,
        capability_grant_id = excluded.capability_grant_id, state = excluded.state,
        acquired_at = excluded.acquired_at, released_at = excluded.released_at, payload = excluded.payload`,
		value.ID.String(),
		value.WorkspaceID.String(),
		value.WorkspacePathSnapshot,
		value.WorkspaceRevision,
		value.OwnerAgentID.String(),
		value.GrantID.String(),
		string(value.State),
		value.AcquiredAt.UTC().Format(time.RFC3339Nano),
		nullableTimeValue(value.ReleasedAt),
		payload,
	)
	if err != nil && isConstraintError(err, "workspace_write_leases.workspace_id") {
		return fmt.Errorf("%w: %s", domain.ErrLeaseConflict, value.WorkspaceID)
	}
	return err
}

func (s *Store) ReleaseWorkspaceLease(ctx context.Context, value domain.WorkspaceWriteLease) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if value.State != domain.LeaseReleased {
		return errors.New("workspace lease release requires a released lease")
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	result, err := executorFromContext(ctx, s.db).ExecContext(
		ctx,
		`UPDATE workspace_write_leases SET state = ?, released_at = ?, payload = ? WHERE id = ? AND state = ?`,
		string(domain.LeaseReleased),
		nullableTimeValue(value.ReleasedAt),
		payload,
		value.ID.String(),
		string(domain.LeaseActive),
	)
	if err != nil {
		return fmt.Errorf("release workspace lease: %w", err)
	}
	return exactlyOne(result, "release workspace lease")
}

func (s *Store) GetWorkspaceLease(ctx context.Context, id domain.WorkspaceLeaseID) (domain.WorkspaceWriteLease, error) {
	row := executorFromContext(
		ctx,
		s.db,
	).QueryRowContext(ctx, `SELECT payload FROM workspace_write_leases WHERE id = ?`, id.String())
	value, err := decodePayload[domain.WorkspaceWriteLease](
		row,
		func(value domain.WorkspaceWriteLease) error { return value.Validate() },
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceWriteLease{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkspaceWriteLease{}, fmt.Errorf("get workspace lease: %w", err)
	}
	return value, nil
}

type WorkspaceLeaseRepository struct{ store *Store }

func (r WorkspaceLeaseRepository) GetActiveByWorkspace(
	ctx context.Context,
	workspaceID domain.WorkspaceID,
) (domain.WorkspaceWriteLease, error) {
	return r.store.GetActiveByWorkspace(ctx, workspaceID)
}

func (r WorkspaceLeaseRepository) Save(ctx context.Context, value domain.WorkspaceWriteLease) error {
	return r.store.SaveWorkspaceLease(ctx, value)
}

func (r WorkspaceLeaseRepository) Release(ctx context.Context, value domain.WorkspaceWriteLease) error {
	return r.store.ReleaseWorkspaceLease(ctx, value)
}
