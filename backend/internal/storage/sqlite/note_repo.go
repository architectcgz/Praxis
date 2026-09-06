package sqlite

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
)

func (s *Store) GetNote(ctx context.Context, id domainfoundation.NoteID) (domainworkflow.Note, error) {
	var value domainworkflow.Note
	return value, s.loadPayload(ctx, "notes", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveNote(ctx context.Context, value domainworkflow.Note) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO notes (id, session_id, source_agent_id, payload) VALUES (?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        session_id = excluded.session_id,
	        source_agent_id = excluded.source_agent_id,
	        payload = excluded.payload`,
		value.ID.String(),
		value.SessionID.String(),
		value.SourceAgentID.String(),
		payload,
	)
}

type NoteRepository struct{ store *Store }

func (r NoteRepository) Get(ctx context.Context, id domainfoundation.NoteID) (domainworkflow.Note, error) {
	return r.store.GetNote(ctx, id)
}

func (r NoteRepository) Save(ctx context.Context, value domainworkflow.Note) error {
	return r.store.SaveNote(ctx, value)
}
