package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
)

func (s *Store) GetCurrentAgentSecurityPolicy(ctx context.Context, agentID domainfoundation.AgentID) (domainsecurity.AgentSecurityPolicy, error) {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM agent_security_policies WHERE agent_id = ? ORDER BY revision DESC LIMIT 1`, agentID.String())
	var ref string
	if err := row.Scan(&ref); err == sql.ErrNoRows {
		return domainsecurity.AgentSecurityPolicy{}, domainfoundation.ErrNotFound
	} else if err != nil {
		return domainsecurity.AgentSecurityPolicy{}, fmt.Errorf("read agent security policy: %w", err)
	}
	var value domainsecurity.AgentSecurityPolicy
	if err := s.loadDocument(ctx, ref, &value, "agent security policy", func() error { return value.Validate() }); err != nil {
		return value, err
	}
	return value, nil
}
func (s *Store) SaveAgentSecurityPolicy(ctx context.Context, agentID domainfoundation.AgentID, value domainsecurity.AgentSecurityPolicy) error {
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

func (r AgentSecurityPolicyRepository) GetCurrent(ctx context.Context, id domainfoundation.AgentID) (domainsecurity.AgentSecurityPolicy, error) {
	return r.store.GetCurrentAgentSecurityPolicy(ctx, id)
}
func (r AgentSecurityPolicyRepository) Save(ctx context.Context, id domainfoundation.AgentID, value domainsecurity.AgentSecurityPolicy) error {
	return r.store.SaveAgentSecurityPolicy(ctx, id, value)
}
func (r AgentSecurityPolicyRepository) GetByRevision(ctx context.Context, id domainfoundation.AgentID, revision uint64) (domainsecurity.AgentSecurityPolicy, bool, error) {
	return r.store.GetAgentSecurityPolicy(ctx, id, revision)
}

// GetAgentSecurityPolicy resolves one revision for policy update idempotency.
func (s *Store) GetAgentSecurityPolicy(ctx context.Context, agentID domainfoundation.AgentID, revision uint64) (domainsecurity.AgentSecurityPolicy, bool, error) {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM agent_security_policies WHERE agent_id = ? AND revision = ?`, agentID.String(), revision)
	var ref string
	if err := row.Scan(&ref); errors.Is(err, sql.ErrNoRows) {
		return domainsecurity.AgentSecurityPolicy{}, false, nil
	} else if err != nil {
		return domainsecurity.AgentSecurityPolicy{}, false, fmt.Errorf("read agent security policy revision: %w", err)
	}
	var value domainsecurity.AgentSecurityPolicy
	if err := s.loadDocument(ctx, ref, &value, "agent security policy", func() error { return value.Validate() }); err != nil {
		return domainsecurity.AgentSecurityPolicy{}, false, err
	}
	return value, true, nil
}
