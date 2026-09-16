package storage

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
)

func (s *Store) GetBriefing(ctx context.Context, id domainfoundation.BriefingID) (domainworkflow.Briefing, error) {
	return loadDocumentRef[domainworkflow.Briefing](ctx, s, `SELECT document_ref FROM briefings WHERE id = ?`, []any{id.String()}, "briefing", func(value domainworkflow.Briefing) error { return value.Validate() })
}
func (s *Store) SaveBriefing(ctx context.Context, value domainworkflow.Briefing) error {
	if err := value.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "briefing", value.ID.String(), value)
	if err != nil {
		return err
	}
	return s.saveMetadata(ctx, `INSERT INTO briefings (id, session_id, source_agent_id, target_agent_id, status, created_at, updated_at, document_ref) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id, source_agent_id = excluded.source_agent_id, target_agent_id = excluded.target_agent_id, status = excluded.status, created_at = excluded.created_at, updated_at = excluded.updated_at, document_ref = excluded.document_ref`, value.ID.String(), value.SessionID.String(), value.SourceAgentID.String(), value.TargetAgentID.String(), string(value.Status), value.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), value.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), ref)
}

type BriefingRepository struct{ store *Store }

func (r BriefingRepository) Get(ctx context.Context, id domainfoundation.BriefingID) (domainworkflow.Briefing, error) {
	return r.store.GetBriefing(ctx, id)
}
func (r BriefingRepository) Save(ctx context.Context, value domainworkflow.Briefing) error {
	return r.store.SaveBriefing(ctx, value)
}
