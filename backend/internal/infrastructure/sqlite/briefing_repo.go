package sqlite

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
)

func (s *Store) GetBriefing(ctx context.Context, id domainfoundation.BriefingID) (domainworkflow.Briefing, error) {
	var value domainworkflow.Briefing
	return value, s.loadPayload(ctx, "briefings", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveBriefing(ctx context.Context, value domainworkflow.Briefing) error {
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
	        id, session_id, source_agent_id, target_agent_id, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        session_id = excluded.session_id,
	        source_agent_id = excluded.source_agent_id,
	        target_agent_id = excluded.target_agent_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.SessionID.String(),
		value.SourceAgentID.String(),
		value.TargetAgentID.String(),
		string(value.Status),
		payload,
	)
}

type BriefingRepository struct{ store *Store }

func (r BriefingRepository) Get(ctx context.Context, id domainfoundation.BriefingID) (domainworkflow.Briefing, error) {
	return r.store.GetBriefing(ctx, id)
}

func (r BriefingRepository) Save(ctx context.Context, value domainworkflow.Briefing) error {
	return r.store.SaveBriefing(ctx, value)
}
