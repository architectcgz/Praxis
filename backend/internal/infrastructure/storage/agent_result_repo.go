package storage

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
)

func (s *Store) GetAgentResult(ctx context.Context, id domainfoundation.AgentResultID) (domainworkflow.AgentResult, error) {
	return loadDocumentRef[domainworkflow.AgentResult](ctx, s, `SELECT document_ref FROM agent_results WHERE id = ?`, []any{id.String()}, "agent result", func(value domainworkflow.AgentResult) error { return value.Validate() })
}
func (s *Store) SaveAgentResult(ctx context.Context, value domainworkflow.AgentResult) error {
	if err := value.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "agent-result", value.ID.String(), value)
	if err != nil {
		return err
	}
	return s.saveMetadata(ctx, `INSERT INTO agent_results (id, session_id, source_agent_id, status, created_at, updated_at, document_ref) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id, source_agent_id = excluded.source_agent_id, status = excluded.status, created_at = excluded.created_at, updated_at = excluded.updated_at, document_ref = excluded.document_ref`, value.ID.String(), value.SessionID.String(), value.SourceAgentID.String(), string(value.Status), value.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), value.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), ref)
}

type AgentResultRepository struct{ store *Store }

func (r AgentResultRepository) Get(ctx context.Context, id domainfoundation.AgentResultID) (domainworkflow.AgentResult, error) {
	return r.store.GetAgentResult(ctx, id)
}
func (r AgentResultRepository) Save(ctx context.Context, value domainworkflow.AgentResult) error {
	return r.store.SaveAgentResult(ctx, value)
}
