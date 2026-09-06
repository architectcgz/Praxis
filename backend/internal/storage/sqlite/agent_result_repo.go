package sqlite

import (
	"context"
	domainworkflow "praxis/internal/domain/workflow"

	domainfoundation "praxis/internal/domain/foundation"
)

func (s *Store) GetAgentResult(ctx context.Context, id domainfoundation.AgentResultID) (domainworkflow.AgentResult, error) {
	var value domainworkflow.AgentResult
	return value, s.loadPayload(ctx, "agent_results", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentResult(ctx context.Context, value domainworkflow.AgentResult) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_results (id, session_id, source_agent_id, status, payload) VALUES (?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        session_id = excluded.session_id,
	        source_agent_id = excluded.source_agent_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.SessionID.String(),
		value.SourceAgentID.String(),
		string(value.Status),
		payload,
	)
}

type AgentResultRepository struct{ store *Store }

func (r AgentResultRepository) Get(ctx context.Context, id domainfoundation.AgentResultID) (domainworkflow.AgentResult, error) {
	return r.store.GetAgentResult(ctx, id)
}

func (r AgentResultRepository) Save(ctx context.Context, value domainworkflow.AgentResult) error {
	return r.store.SaveAgentResult(ctx, value)
}
