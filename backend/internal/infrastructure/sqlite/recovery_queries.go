package sqlite

import (
	"context"
	"fmt"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
	domainworkspace "praxis/internal/domain/workspace"
)

// RecoveryAgentRef identifies an Agent transcript that may need reconciliation.
type RecoveryAgentRef struct {
	SessionID domainfoundation.SessionID
	AgentID   domainfoundation.AgentID
}

func (s *Store) ListRecoveryAgents(ctx context.Context) ([]RecoveryAgentRef, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(
		ctx,
		`SELECT session_id, id FROM agents ORDER BY session_id, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list recovery agents: %w", err)
	}
	defer rows.Close()
	refs := make([]RecoveryAgentRef, 0)
	for rows.Next() {
		var sessionID, agentID string
		if err := rows.Scan(&sessionID, &agentID); err != nil {
			return nil, fmt.Errorf("scan recovery agent: %w", err)
		}
		refs = append(refs, RecoveryAgentRef{
			SessionID: domainfoundation.SessionID(sessionID),
			AgentID:   domainfoundation.AgentID(agentID),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovery agents: %w", err)
	}
	return refs, nil
}

func (s *Store) ListUnsettledExecutionIDs(ctx context.Context) ([]domainfoundation.AgentExecutionID, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(
		ctx,
		`SELECT id FROM agent_executions WHERE status <> 'settled' ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list unsettled executions: %w", err)
	}
	defer rows.Close()
	ids := make([]domainfoundation.AgentExecutionID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan unsettled execution: %w", err)
		}
		ids = append(ids, domainfoundation.AgentExecutionID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unsettled executions: %w", err)
	}
	return ids, nil
}

func (s *Store) ListActiveLeaseIDs(ctx context.Context) ([]domainfoundation.WorkspaceLeaseID, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(
		ctx,
		`SELECT id FROM workspace_write_leases WHERE state = ? ORDER BY id`,
		string(domainworkspace.LeaseActive),
	)
	if err != nil {
		return nil, fmt.Errorf("list active workspace leases: %w", err)
	}
	defer rows.Close()
	ids := make([]domainfoundation.WorkspaceLeaseID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan active workspace lease: %w", err)
		}
		ids = append(ids, domainfoundation.WorkspaceLeaseID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active workspace leases: %w", err)
	}
	return ids, nil
}

func (s *Store) ListInFlightDeliveryIDs(ctx context.Context) ([]domainfoundation.DeliveryID, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(
		ctx,
		`SELECT id FROM context_deliveries WHERE status IN (?, ?) ORDER BY id`,
		string(domainworkflow.ContextDeliveryPending), string(domainworkflow.ContextDeliveryDelivering),
	)
	if err != nil {
		return nil, fmt.Errorf("list in-flight deliveries: %w", err)
	}
	defer rows.Close()
	ids := make([]domainfoundation.DeliveryID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan in-flight delivery: %w", err)
		}
		ids = append(ids, domainfoundation.DeliveryID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate in-flight deliveries: %w", err)
	}
	return ids, nil
}
