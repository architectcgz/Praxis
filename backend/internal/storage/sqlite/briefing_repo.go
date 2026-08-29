package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetBriefing(ctx context.Context, id domain.BriefingID) (domain.Briefing, error) {
	var value domain.Briefing
	return value, s.loadPayload(ctx, "briefings", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveBriefing(ctx context.Context, value domain.Briefing) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO briefings (
	        id, task_session_id, source_thread_id, target_thread_id, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        target_thread_id = excluded.target_thread_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		value.TargetThreadID.String(),
		string(value.Status),
		payload,
	)
}

type BriefingRepository struct{ store *Store }

func (r BriefingRepository) Get(ctx context.Context, id domain.BriefingID) (domain.Briefing, error) {
	return r.store.GetBriefing(ctx, id)
}

func (r BriefingRepository) Save(ctx context.Context, value domain.Briefing) error {
	return r.store.SaveBriefing(ctx, value)
}
