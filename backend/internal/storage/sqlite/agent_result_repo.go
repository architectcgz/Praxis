package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetAgentResult(ctx context.Context, id domain.AgentResultID) (domain.AgentResult, error) {
	var value domain.AgentResult
	return value, s.loadPayload(ctx, "agent_results", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentResult(ctx context.Context, value domain.AgentResult) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_results (id, task_session_id, source_thread_id, status, payload) VALUES (?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		string(value.Status),
		payload,
	)
}

type AgentResultRepository struct{ store *Store }

func (r AgentResultRepository) Get(ctx context.Context, id domain.AgentResultID) (domain.AgentResult, error) {
	return r.store.GetAgentResult(ctx, id)
}

func (r AgentResultRepository) Save(ctx context.Context, value domain.AgentResult) error {
	return r.store.SaveAgentResult(ctx, value)
}
