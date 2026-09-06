package orchestrate

import (
	"context"
	"encoding/json"
	"errors"
	domaincommand "praxis/internal/core/domain/command"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
	"strconv"
)

type UpdateAgentSecurityPolicyRequest struct {
	RequestID        domainfoundation.RequestID
	AgentID          domainfoundation.AgentID
	ExpectedRevision uint64
	Policy           domainsecurity.AgentSecurityPolicy
}

type UpdateAgentSecurityPolicyResult struct {
	AgentID         domainfoundation.AgentID
	Policy          domainsecurity.AgentSecurityPolicy
	ExistingRequest bool
}

func (o *AgentOrchestrator) UpdateAgentSecurityPolicy(ctx context.Context, request UpdateAgentSecurityPolicyRequest) (UpdateAgentSecurityPolicyResult, error) {
	if ctx == nil {
		return UpdateAgentSecurityPolicyResult{}, errors.New("update agent security policy context is required")
	}
	if !o.Ready() || request.RequestID == "" || request.AgentID == "" || request.ExpectedRevision == 0 {
		return UpdateAgentSecurityPolicyResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.Policy.Validate(); err != nil {
		return UpdateAgentSecurityPolicyResult{}, commandError(CommandErrorInvalidRequest)
	}
	digest := commandArgumentsDigest(struct {
		AgentID  domainfoundation.AgentID
		Expected uint64
		Policy   domainsecurity.AgentSecurityPolicy
	}{request.AgentID, request.ExpectedRevision, request.Policy})
	var result UpdateAgentSecurityPolicyResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		if receipt, found, err := commandReceipt(txCtx, o.commandReceipts, request.RequestID, "update_agent_security_policy", digest); err != nil {
			return err
		} else if found {
			var value struct {
				AgentID  string
				Revision uint64
			}
			if err := json.Unmarshal(receipt.ResultPayload, &value); err != nil {
				return err
			}
			policy, err := o.policies.GetCurrent(txCtx, domainfoundation.AgentID(value.AgentID))
			if err != nil {
				return err
			}
			result = UpdateAgentSecurityPolicyResult{AgentID: domainfoundation.AgentID(value.AgentID), Policy: policy, ExistingRequest: true}
			return nil
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		if agent.SecurityPolicyRevision != request.ExpectedRevision || request.Policy.Revision != request.ExpectedRevision+1 {
			return domainfoundation.ErrRevisionConflict
		}
		if err := o.policies.Save(txCtx, agent.ID, request.Policy); err != nil {
			return err
		}
		agent.SecurityPolicyRevision = request.Policy.Revision
		agent.UpdatedAt = o.clock.Now().UTC()
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		event := o.newEvent(domainfoundation.EventAgentPolicyUpdated, agent.UpdatedAt)
		event.SessionID, event.AgentID = agent.SessionID, agent.ID
		event.Payload = map[string]string{"revision": strconv.FormatUint(request.Policy.Revision, 10)}
		if err := o.appendEvent(txCtx, event); err != nil {
			return err
		}
		if o.commandReceipts != nil {
			payload, _ := json.Marshal(struct {
				AgentID  string
				Revision uint64
			}{agent.ID.String(), request.Policy.Revision})
			if err := o.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{RequestID: request.RequestID, Command: "update_agent_security_policy", ArgumentsDigest: digest, ResultPayload: payload, CreatedAt: agent.UpdatedAt}); err != nil {
				return err
			}
		}
		result = UpdateAgentSecurityPolicyResult{AgentID: agent.ID, Policy: request.Policy}
		return nil
	})
	return result, err
}
