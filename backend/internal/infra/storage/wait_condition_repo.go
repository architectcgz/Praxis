package storage

import (
	"praxis/internal/contracts"
	workflowmodel "praxis/internal/workflow"

	"context"
)

func (s *Store) GetWaitCondition(ctx context.Context, id contracts.WaitConditionID) (workflowmodel.WaitCondition, error) {
	return loadDocumentRef[workflowmodel.WaitCondition](ctx, s, `SELECT document_ref FROM wait_conditions WHERE id = ?`, []any{id.String()}, "wait condition", func(value workflowmodel.WaitCondition) error { return value.Validate() })
}
func (s *Store) SaveWaitCondition(ctx context.Context, value workflowmodel.WaitCondition) error {
	if err := value.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "wait", value.ID.String(), value)
	if err != nil {
		return err
	}
	return s.saveMetadata(ctx, `INSERT INTO wait_conditions (id, agent_id, execution_id, kind, mode, status, document_ref) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id, execution_id = excluded.execution_id, kind = excluded.kind, mode = excluded.mode, status = excluded.status, document_ref = excluded.document_ref`, value.ID.String(), value.AgentID.String(), value.ExecutionID.String(), string(value.Kind), string(value.Mode), string(value.Status), ref)
}
func (s *Store) ListUnresolvedWaitConditionsByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]workflowmodel.WaitCondition, error) {
	return listDocumentRefs[workflowmodel.WaitCondition](ctx, s, `SELECT document_ref FROM wait_conditions WHERE agent_id = ? AND status = 'pending' ORDER BY id LIMIT ?`, []any{agentID.String(), targetLimit(limit)}, "unresolved wait conditions", func(value workflowmodel.WaitCondition) error { return value.Validate() })
}

type WaitConditionRepository struct{ store *Store }

func (r WaitConditionRepository) Get(ctx context.Context, id contracts.WaitConditionID) (workflowmodel.WaitCondition, error) {
	return r.store.GetWaitCondition(ctx, id)
}
func (r WaitConditionRepository) Save(ctx context.Context, value workflowmodel.WaitCondition) error {
	return r.store.SaveWaitCondition(ctx, value)
}
func (r WaitConditionRepository) ListUnresolvedByAgent(ctx context.Context, id contracts.AgentID, limit int) ([]workflowmodel.WaitCondition, error) {
	return r.store.ListUnresolvedWaitConditionsByAgent(ctx, id, limit)
}
