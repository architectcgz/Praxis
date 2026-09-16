package storage

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
)

func (s *Store) GetNote(ctx context.Context, id domainfoundation.NoteID) (domainworkflow.Note, error) {
	return loadDocumentRef[domainworkflow.Note](ctx, s, `SELECT document_ref FROM notes WHERE id = ?`, []any{id.String()}, "note", func(value domainworkflow.Note) error { return value.Validate() })
}
func (s *Store) SaveNote(ctx context.Context, value domainworkflow.Note) error {
	if err := value.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "note", value.ID.String(), value)
	if err != nil {
		return err
	}
	return s.saveMetadata(ctx, `INSERT INTO notes (id, session_id, source_agent_id, created_at, document_ref) VALUES (?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id, source_agent_id = excluded.source_agent_id, created_at = excluded.created_at, document_ref = excluded.document_ref`, value.ID.String(), value.SessionID.String(), value.SourceAgentID.String(), value.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), ref)
}

type NoteRepository struct{ store *Store }

func (r NoteRepository) Get(ctx context.Context, id domainfoundation.NoteID) (domainworkflow.Note, error) {
	return r.store.GetNote(ctx, id)
}
func (r NoteRepository) Save(ctx context.Context, value domainworkflow.Note) error {
	return r.store.SaveNote(ctx, value)
}
