package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetAgentThread(ctx context.Context, id domain.AgentThreadID) (domain.AgentThread, error) {
	var value domain.AgentThread
	return value, s.loadPayload(ctx, "agent_threads", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentThread(ctx context.Context, value domain.AgentThread) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_threads (
        id, task_session_id, profile, task_packet_id, context_manifest_id, capability_grant_id, state, payload
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET task_session_id = excluded.task_session_id, profile = excluded.profile,
        task_packet_id = excluded.task_packet_id, context_manifest_id = excluded.context_manifest_id,
        capability_grant_id = excluded.capability_grant_id, state = excluded.state, payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		string(value.Profile),
		value.TaskPacketID.String(),
		value.ContextManifestID.String(),
		value.GrantID.String(),
		string(value.State),
		payload,
	)
}

type AgentThreadRepository struct{ store *Store }

func (r AgentThreadRepository) Get(ctx context.Context, id domain.AgentThreadID) (domain.AgentThread, error) {
	return r.store.GetAgentThread(ctx, id)
}

func (r AgentThreadRepository) Save(ctx context.Context, value domain.AgentThread) error {
	return r.store.SaveAgentThread(ctx, value)
}
