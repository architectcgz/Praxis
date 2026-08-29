package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetNote(ctx context.Context, id domain.NoteID) (domain.Note, error) {
	var value domain.Note
	return value, s.loadPayload(ctx, "notes", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveNote(ctx context.Context, value domain.Note) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO notes (id, task_session_id, source_thread_id, payload) VALUES (?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		payload,
	)
}

type NoteRepository struct{ store *Store }

func (r NoteRepository) Get(ctx context.Context, id domain.NoteID) (domain.Note, error) {
	return r.store.GetNote(ctx, id)
}

func (r NoteRepository) Save(ctx context.Context, value domain.Note) error {
	return r.store.SaveNote(ctx, value)
}
