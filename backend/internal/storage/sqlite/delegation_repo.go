package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetDelegation(ctx context.Context, id domain.DelegationRequestID) (domain.DelegationRequest, error) {
	var value domain.DelegationRequest
	return value, s.loadPayload(
		ctx,
		"delegation_requests",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveDelegation(ctx context.Context, value domain.DelegationRequest) error {
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
	        id, task_session_id, source_thread_id, profile, task_packet_id,
	        context_manifest_id, capability_grant_id, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        profile = excluded.profile,
	        task_packet_id = excluded.task_packet_id,
	        context_manifest_id = excluded.context_manifest_id,
	        capability_grant_id = excluded.capability_grant_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		string(value.Profile),
		value.TaskPacketID.String(),
		value.ManifestID.String(),
		value.Grant.ID.String(),
		string(value.Status),
		payload,
	)
}

type DelegationRepository struct{ store *Store }

func (r DelegationRepository) Get(
	ctx context.Context,
	id domain.DelegationRequestID,
) (domain.DelegationRequest, error) {
	return r.store.GetDelegation(ctx, id)
}

func (r DelegationRepository) Save(ctx context.Context, value domain.DelegationRequest) error {
	return r.store.SaveDelegation(ctx, value)
}
