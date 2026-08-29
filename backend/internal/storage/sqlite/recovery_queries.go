package sqlite

import (
	"context"
	"fmt"

	"praxis/internal/core/domain"
)

// RecoveryThreadRef identifies a session file that must be checked before
// product state is marked interrupted during process-start reconciliation.
type RecoveryThreadRef struct {
	TaskSessionID domain.TaskSessionID
	AgentThreadID domain.AgentThreadID
}

func (s *Store) ListRecoveryThreads(ctx context.Context) ([]RecoveryThreadRef, error) {
	rows, err := executorFromContext(
		ctx,
		s.db,
	).QueryContext(ctx, `SELECT task_session_id, id FROM agent_threads ORDER BY task_session_id, id`)
	if err != nil {
		return nil, fmt.Errorf("list recovery threads: %w", err)
	}
	defer rows.Close()
	refs := make([]RecoveryThreadRef, 0)
	for rows.Next() {
		var sessionID, threadID string
		if err := rows.Scan(&sessionID, &threadID); err != nil {
			return nil, fmt.Errorf("scan recovery thread: %w", err)
		}
		refs = append(
			refs,
			RecoveryThreadRef{
				TaskSessionID: domain.TaskSessionID(sessionID),
				AgentThreadID: domain.AgentThreadID(threadID),
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovery threads: %w", err)
	}
	return refs, nil
}

func (s *Store) ListUnsettledRunIDs(ctx context.Context) ([]domain.AgentRunID, error) {
	rows, err := executorFromContext(
		ctx,
		s.db,
	).QueryContext(ctx, `SELECT id FROM agent_runs WHERE outcome = '' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list unsettled runs: %w", err)
	}
	defer rows.Close()
	ids := make([]domain.AgentRunID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan unsettled run: %w", err)
		}
		ids = append(ids, domain.AgentRunID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unsettled runs: %w", err)
	}
	return ids, nil
}

func (s *Store) ListActiveLeaseIDs(ctx context.Context) ([]domain.WorkspaceLeaseID, error) {
	rows, err := executorFromContext(
		ctx,
		s.db,
	).QueryContext(ctx, `SELECT id FROM workspace_write_leases WHERE state = ? ORDER BY id`, string(domain.LeaseActive))
	if err != nil {
		return nil, fmt.Errorf("list active workspace leases: %w", err)
	}
	defer rows.Close()
	ids := make([]domain.WorkspaceLeaseID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan active workspace lease: %w", err)
		}
		ids = append(ids, domain.WorkspaceLeaseID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active workspace leases: %w", err)
	}
	return ids, nil
}

func (s *Store) ListInFlightDeliveryIDs(ctx context.Context) ([]domain.DeliveryID, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(
		ctx,
		`SELECT id FROM briefing_deliveries WHERE status IN (?, ?) ORDER BY id`,
		string(domain.DeliveryPending),
		string(domain.DeliveryDelivering),
	)
	if err != nil {
		return nil, fmt.Errorf("list in-flight deliveries: %w", err)
	}
	defer rows.Close()
	ids := make([]domain.DeliveryID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan in-flight delivery: %w", err)
		}
		ids = append(ids, domain.DeliveryID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate in-flight deliveries: %w", err)
	}
	return ids, nil
}
