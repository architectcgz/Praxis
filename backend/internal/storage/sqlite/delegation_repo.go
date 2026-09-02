package sqlite

import (
	"context"

	domainfoundation "praxis/internal/core/domain/foundation"
	domainworkflow "praxis/internal/core/domain/workflow"
)

func (s *Store) GetDelegation(ctx context.Context, id domainfoundation.DelegationRequestID) (domainworkflow.DelegationRequest, error) {
	var value domainworkflow.DelegationRequest
	return value, s.loadPayload(
		ctx,
		"delegation_requests",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveDelegation(ctx context.Context, value domainworkflow.DelegationRequest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO delegation_requests (
	        id, session_id, source_agent_id, profile,
	        context_manifest_id, capability_grant_id, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        session_id = excluded.session_id,
	        source_agent_id = excluded.source_agent_id,
	        profile = excluded.profile,
	        context_manifest_id = excluded.context_manifest_id,
	        capability_grant_id = excluded.capability_grant_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.SessionID.String(),
		value.SourceAgentID.String(),
		string(value.Profile),
		value.ManifestID.String(),
		value.Grant.ID.String(),
		string(value.Status),
		payload,
	)
}

type DelegationRepository struct{ store *Store }

func (r DelegationRepository) Get(
	ctx context.Context,
	id domainfoundation.DelegationRequestID,
) (domainworkflow.DelegationRequest, error) {
	return r.store.GetDelegation(ctx, id)
}

func (r DelegationRepository) Save(ctx context.Context, value domainworkflow.DelegationRequest) error {
	return r.store.SaveDelegation(ctx, value)
}
