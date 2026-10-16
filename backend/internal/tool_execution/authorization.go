package toolexecution

import (
	"context"
	"encoding/json"
	"errors"

	"praxis/internal/contracts"
	securitymodel "praxis/internal/core/security"
	toolmodel "praxis/internal/core/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"
)

// checkTask 固定执行归属与工作区，不冻结或求交集命令策略。
func (s *Executor) checkTask(ctx context.Context, provided contracts.ToolInvocationContext) error {
	task, err := s.tasks.Get(ctx, provided.TaskID)
	if err != nil {
		return err
	}
	if !task.Active() || task.SessionID != provided.SessionID || task.AgentID != provided.AgentID ||
		task.Config.WorkspacePath != provided.WorkspacePath {
		return contracts.ErrRequestConflict
	}
	return nil
}

func (s *Executor) commandDecision(invocation toolmodel.ToolInvocation) (securitymodel.CommandDecision, error) {
	if invocation.Name != contracts.ToolBash {
		return securitymodel.CommandDecision{Action: securitymodel.CommandAllow}, nil
	}
	var arguments struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(invocation.NormalizedArguments, &arguments); err != nil || arguments.Command == "" {
		return securitymodel.CommandDecision{}, errors.New("normalized bash command is required")
	}
	return s.commandPolicy.DecideCommand(arguments.Command), nil
}

func (s *Executor) admit(ctx context.Context, invocation toolmodel.ToolInvocation, call toolcontracts.NormalizedToolCall) (toolmodel.ToolInvocation, error) {
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, invocation.ID)
		if err != nil {
			return err
		}
		if current.Status != toolmodel.ToolInvocationRequested {
			invocation = current
			return nil
		}
		current.NormalizedArguments = append(json.RawMessage(nil), call.NormalizedArguments...)
		current.Path = call.Path
		decision, err := s.commandDecision(current)
		if err != nil {
			return err
		}
		if _, enabled := s.toolCatalog.Get(current.Name); !enabled {
			decision = securitymodel.CommandDecision{Action: securitymodel.CommandDeny, RuleID: "tool-disabled"}
		}
		if current.Name == contracts.ToolBash {
			current.CommandDecision = decision
		}
		switch decision.Action {
		case securitymodel.CommandDeny:
			result := toolcontracts.NewToolError(string(toolmodel.ToolFailureNotAllowed), "The tool call is denied by the current configuration. Rule: "+decision.RuleID)
			err = current.Deny(toolmodel.ToolInvocationResult{InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureNotAllowed}, s.clock.Now())
		case securitymodel.CommandAsk:
			err = current.AwaitApproval()
		case securitymodel.CommandAllow:
			var approval contracts.ApprovalRecord
			approval, err = contracts.NewPolicyApproval(toolmodel.ArgumentsDigest(current.Name, current.NormalizedArguments), s.clock.Now())
			if err == nil {
				err = current.Approve(approval)
			}
		}
		if err != nil {
			return err
		}
		if err := s.invocations.Save(txCtx, current); err != nil {
			return err
		}
		invocation = current
		return nil
	})
	return invocation, err
}
