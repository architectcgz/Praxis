package sqlite

import (
	"context"
	"time"

	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
)

func (s *Store) GetCurrentAgentSecurityPolicy(ctx context.Context, agentID domainfoundation.AgentID) (domainsecurity.AgentSecurityPolicy, error) {
	return loadTargetPayload[domainsecurity.AgentSecurityPolicy](ctx, s,
		`SELECT payload FROM agent_security_policies WHERE agent_id = ? ORDER BY revision DESC LIMIT 1`,
		[]any{agentID.String()}, "agent security policy",
		func(value domainsecurity.AgentSecurityPolicy) error { return value.Validate() })
}

func (s *Store) SaveAgentSecurityPolicy(ctx context.Context, agentID domainfoundation.AgentID, value domainsecurity.AgentSecurityPolicy) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(ctx,
		`INSERT INTO agent_security_policies (agent_id, revision, created_at, payload) VALUES (?, ?, ?, ?)`,
		agentID.String(), value.Revision, s.clock.Now().UTC().Format(time.RFC3339Nano), payload)
}

type AgentSecurityPolicyRepository struct{ store *Store }

func (r AgentSecurityPolicyRepository) GetCurrent(ctx context.Context, id domainfoundation.AgentID) (domainsecurity.AgentSecurityPolicy, error) {
	return r.store.GetCurrentAgentSecurityPolicy(ctx, id)
}

func (r AgentSecurityPolicyRepository) Save(ctx context.Context, id domainfoundation.AgentID, value domainsecurity.AgentSecurityPolicy) error {
	return r.store.SaveAgentSecurityPolicy(ctx, id, value)
}
