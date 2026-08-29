package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetAgentRun(ctx context.Context, id domain.AgentRunID) (domain.AgentRun, error) {
	var value domain.AgentRun
	return value, s.loadPayload(ctx, "agent_runs", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentRun(ctx context.Context, value domain.AgentRun) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_runs (
	        id, agent_thread_id, work_item_id, sandbox_mode, approval_mode, outcome, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        agent_thread_id = excluded.agent_thread_id,
	        work_item_id = excluded.work_item_id,
	        sandbox_mode = excluded.sandbox_mode,
	        approval_mode = excluded.approval_mode,
	        outcome = excluded.outcome,
	        payload = excluded.payload`,
		value.ID.String(),
		value.AgentThreadID.String(),
		value.WorkItemID.String(),
		string(value.Execution.SandboxMode),
		string(value.Execution.ApprovalMode),
		string(value.Outcome),
		payload,
	)
}

type AgentRunRepository struct{ store *Store }

func (r AgentRunRepository) Get(ctx context.Context, id domain.AgentRunID) (domain.AgentRun, error) {
	return r.store.GetAgentRun(ctx, id)
}

func (r AgentRunRepository) Save(ctx context.Context, value domain.AgentRun) error {
	return r.store.SaveAgentRun(ctx, value)
}
