package storage

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
)

func (s *Store) GetWaitCondition(ctx context.Context, id domainfoundation.WaitConditionID) (domainworkflow.WaitCondition, error) {
	return loadDocumentRef[domainworkflow.WaitCondition](ctx, s, `SELECT document_ref FROM wait_conditions WHERE id = ?`, []any{id.String()}, "wait condition", func(value domainworkflow.WaitCondition) error { return value.Validate() })
}
func (s *Store) SaveWaitCondition(ctx context.Context, value domainworkflow.WaitCondition) error {
	if err := value.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "wait", value.ID.String(), value)
	if err != nil {
		return err
	}
	return s.saveMetadata(ctx, `INSERT INTO wait_conditions (id, agent_id, execution_id, kind, mode, status, document_ref) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id, execution_id = excluded.execution_id, kind = excluded.kind, mode = excluded.mode, status = excluded.status, document_ref = excluded.document_ref`, value.ID.String(), value.AgentID.String(), value.ExecutionID.String(), string(value.Kind), string(value.Mode), string(value.Status), ref)
}
func (s *Store) ListUnresolvedWaitConditionsByAgent(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]domainworkflow.WaitCondition, error) {
	return listDocumentRefs[domainworkflow.WaitCondition](ctx, s, `SELECT document_ref FROM wait_conditions WHERE agent_id = ? AND status = 'pending' ORDER BY id LIMIT ?`, []any{agentID.String(), targetLimit(limit)}, "unresolved wait conditions", func(value domainworkflow.WaitCondition) error { return value.Validate() })
}
func (s *Store) ListUnresolvedWaitConditionsByAgentAfter(ctx context.Context, agentID domainfoundation.AgentID, afterID domainfoundation.WaitConditionID, limit int) ([]domainworkflow.WaitCondition, error) {
	return listDocumentRefs[domainworkflow.WaitCondition](ctx, s, `SELECT document_ref FROM wait_conditions WHERE agent_id = ? AND status = 'pending' AND id > ? ORDER BY id LIMIT ?`, []any{agentID.String(), afterID.String(), targetLimit(limit)}, "unresolved wait conditions", func(value domainworkflow.WaitCondition) error { return value.Validate() })
}

type WaitConditionRepository struct{ store *Store }

func (r WaitConditionRepository) Get(ctx context.Context, id domainfoundation.WaitConditionID) (domainworkflow.WaitCondition, error) {
	return r.store.GetWaitCondition(ctx, id)
}
func (r WaitConditionRepository) Save(ctx context.Context, value domainworkflow.WaitCondition) error {
	return r.store.SaveWaitCondition(ctx, value)
}
func (r WaitConditionRepository) ListUnresolvedByAgent(ctx context.Context, id domainfoundation.AgentID, limit int) ([]domainworkflow.WaitCondition, error) {
	return r.store.ListUnresolvedWaitConditionsByAgent(ctx, id, limit)
}
func (r WaitConditionRepository) ListUnresolvedByAgentAfter(ctx context.Context, id domainfoundation.AgentID, after domainfoundation.WaitConditionID, limit int) ([]domainworkflow.WaitCondition, error) {
	return r.store.ListUnresolvedWaitConditionsByAgentAfter(ctx, id, after, limit)
}
