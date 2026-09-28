package storage

import (
	"praxis/internal/contracts"
	securitymodel "praxis/internal/security"

	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Store) GetCurrentAgentSecurityPolicy(ctx context.Context, agentID contracts.AgentID) (securitymodel.AgentSecurityPolicy, error) {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM agent_security_policies WHERE agent_id = ? ORDER BY revision DESC LIMIT 1`, agentID.String())
	var ref string
	if err := row.Scan(&ref); err == sql.ErrNoRows {
		return securitymodel.AgentSecurityPolicy{}, contracts.ErrNotFound
	} else if err != nil {
		return securitymodel.AgentSecurityPolicy{}, fmt.Errorf("read agent security policy: %w", err)
	}
	var value securitymodel.AgentSecurityPolicy
	if err := s.loadDocument(ctx, ref, &value, "agent security policy", func() error { return value.Validate() }); err != nil {
		return value, err
	}
	return value, nil
}
func (s *Store) SaveAgentSecurityPolicy(ctx context.Context, agentID contracts.AgentID, value securitymodel.AgentSecurityPolicy) error {
	if err := value.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "agent-policy", agentID.String()+"-"+fmt.Sprint(value.Revision), value)
	if err != nil {
		return err
	}
	return s.saveMetadata(ctx, `INSERT INTO agent_security_policies (agent_id, revision, created_at, document_ref) VALUES (?, ?, ?, ?)`, agentID.String(), value.Revision, s.db.Now().UTC().Format(time.RFC3339Nano), ref)
}

type AgentSecurityPolicyRepository struct{ store *Store }

func (r AgentSecurityPolicyRepository) GetCurrent(ctx context.Context, id contracts.AgentID) (securitymodel.AgentSecurityPolicy, error) {
	return r.store.GetCurrentAgentSecurityPolicy(ctx, id)
}
func (r AgentSecurityPolicyRepository) Save(ctx context.Context, id contracts.AgentID, value securitymodel.AgentSecurityPolicy) error {
	return r.store.SaveAgentSecurityPolicy(ctx, id, value)
}
func (r AgentSecurityPolicyRepository) GetByRevision(ctx context.Context, id contracts.AgentID, revision uint64) (securitymodel.AgentSecurityPolicy, bool, error) {
	return r.store.GetAgentSecurityPolicy(ctx, id, revision)
}

// GetAgentSecurityPolicy resolves one revision for policy update idempotency.
func (s *Store) GetAgentSecurityPolicy(ctx context.Context, agentID contracts.AgentID, revision uint64) (securitymodel.AgentSecurityPolicy, bool, error) {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM agent_security_policies WHERE agent_id = ? AND revision = ?`, agentID.String(), revision)
	var ref string
	if err := row.Scan(&ref); errors.Is(err, sql.ErrNoRows) {
		return securitymodel.AgentSecurityPolicy{}, false, nil
	} else if err != nil {
		return securitymodel.AgentSecurityPolicy{}, false, fmt.Errorf("read agent security policy revision: %w", err)
	}
	var value securitymodel.AgentSecurityPolicy
	if err := s.loadDocument(ctx, ref, &value, "agent security policy", func() error { return value.Validate() }); err != nil {
		return securitymodel.AgentSecurityPolicy{}, false, err
	}
	return value, true, nil
}
