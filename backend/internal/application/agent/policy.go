// Package agent owns Agent policy and structured result write use cases.
package agent

import (
	"context"
	"errors"
	"fmt"

	commandprotocol "praxis/internal/command"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
	"praxis/internal/persistence"
	"praxis/internal/system"
)

type Readiness interface{ Ready() bool }

type Config struct {
	Transactions persistence.TxRunner
	Agents       persistence.AgentRepository
	Policies     persistence.AgentSecurityPolicyRepository
	Events       persistence.EventRepository
	Readiness    Readiness
	Clock        system.Clock
	IDs          system.IDGenerator
}

type Service struct {
	tx        persistence.TxRunner
	agents    persistence.AgentRepository
	policies  persistence.AgentSecurityPolicyRepository
	events    persistence.EventRepository
	readiness Readiness
	clock     system.Clock
	ids       system.IDGenerator
}

type UpdatePolicyParams struct {
	RequestID        domainfoundation.RequestID
	AgentID          domainfoundation.AgentID
	ExpectedRevision uint64
	Policy           domainsecurity.AgentSecurityPolicy
}

type UpdatePolicyResult struct {
	AgentID         domainfoundation.AgentID
	Policy          domainsecurity.AgentSecurityPolicy
	ExistingRequest bool
}

func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{"transactions", config.Transactions}, {"agents", config.Agents}, {"security policies", config.Policies},
		{"events", config.Events}, {"readiness", config.Readiness},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("agent policy service %s is required", required.name)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	ids := config.IDs
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	return &Service{tx: config.Transactions, agents: config.Agents, policies: config.Policies, events: config.Events, readiness: config.Readiness, clock: clock, ids: ids}, nil
}

// UpdatePolicy atomically writes one policy revision, Agent revision pointer,
// audit event and idempotency receipt.
func (s *Service) UpdatePolicy(ctx context.Context, params UpdatePolicyParams) (UpdatePolicyResult, error) {
	if ctx == nil {
		return UpdatePolicyResult{}, errors.New("update agent security policy context is required")
	}
	if !s.readiness.Ready() || params.RequestID == "" || params.AgentID == "" || params.ExpectedRevision == 0 {
		return UpdatePolicyResult{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	if err := params.Policy.Validate(); err != nil {
		return UpdatePolicyResult{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	result := UpdatePolicyResult{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, found, err := s.policies.GetByRevision(txCtx, params.AgentID, params.Policy.Revision)
		if err != nil {
			return err
		}
		if found {
			if commandprotocol.ArgumentsDigest(existing) != commandprotocol.ArgumentsDigest(params.Policy) {
				return domainfoundation.ErrRequestConflict
			}
			result = UpdatePolicyResult{AgentID: params.AgentID, Policy: existing, ExistingRequest: true}
			return nil
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if agent.SecurityPolicyRevision != params.ExpectedRevision || params.Policy.Revision != params.ExpectedRevision+1 {
			return domainfoundation.ErrRevisionConflict
		}
		if err := s.policies.Save(txCtx, agent.ID, params.Policy); err != nil {
			return err
		}
		agent.SecurityPolicyRevision = params.Policy.Revision
		agent.UpdatedAt = s.clock.Now().UTC()
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		event := domainfoundation.NewDomainEvent(domainfoundation.EventAgentPolicyUpdated, agent.UpdatedAt)
		event.ID = domainfoundation.EventID(s.ids.New("event"))
		event.SessionID, event.AgentID = agent.SessionID, agent.ID
		event.PolicyRevision = params.Policy.Revision
		if err := s.events.Append(txCtx, event); err != nil {
			return err
		}
		result = UpdatePolicyResult{AgentID: agent.ID, Policy: params.Policy}
		return nil
	})
	return result, err
}
