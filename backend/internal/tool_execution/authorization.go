package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"praxis/internal/contracts"
	securitymodel "praxis/internal/core/security"
	toolmodel "praxis/internal/core/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"
)

func (s *Executor) loadSecurity(
	ctx context.Context,
	provided contracts.ToolInvocationContext,
) (contracts.SecuritySnapshot, error) {
	task, err := s.tasks.Get(ctx, provided.TaskID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	if !task.Active() || task.SessionID != provided.SessionID || task.AgentID != provided.AgentID {
		return contracts.SecuritySnapshot{}, errors.New(
			"tool invocation task is not active or ownership does not match",
		)
	}
	security := task.Input.Security
	if security.Fingerprint != task.Input.Security.Fingerprint ||
		security.Fingerprint != provided.SecurityFingerprint ||
		provided.WorkspacePath != task.Input.WorkspacePath {
		return contracts.SecuritySnapshot{}, errors.New(
			"tool invocation security snapshot does not match the active task",
		)
	}
	current, err := s.currentSecurity.ResolveTaskSecurity(ctx, task)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	return securitymodel.IntersectSnapshot(security, current), nil
}

func (s *Executor) admit(
	ctx context.Context,
	invocation toolmodel.ToolInvocation,
	security contracts.SecuritySnapshot,
	call toolcontracts.NormalizedToolCall,
) (toolmodel.ToolInvocation, error) {
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, invocation.ID)
		if err != nil {
			return err
		}
		if current.Status != toolmodel.ToolInvocationRequested {
			invocation = current
			return nil
		}
		allowed := securitymodel.AllowsToolCall(security, current.Name, call.Path)
		if !allowed {
			deniedResult := toolcontracts.NewToolError(
				string(toolmodel.ToolFailureNotAllowed),
				"The requested path is outside the authorized read scope.",
			)
			denied := toolmodel.ToolInvocationResult{
				InlineContent: deniedResult.Payload,
				ErrorCode:     toolmodel.ToolFailureNotAllowed,
			}
			if err := current.Deny(denied, s.clock.Now()); err != nil {
				return err
			}
		} else {
			current.NormalizedArguments = append(json.RawMessage(nil), call.NormalizedArguments...)
			current.Path = call.Path

			if securitymodel.RequiresToolApproval(security, current.Name) {
				if err := current.AwaitApproval(); err != nil {
					return err
				}
			} else {
				approval, err := contracts.NewPolicyApproval(security.Fingerprint, s.clock.Now())
				if err != nil {
					return err
				}
				if err := current.Approve(approval); err != nil {
					return err
				}
			}
		}
		if err := s.invocations.Save(txCtx, current); err != nil {
			return err
		}
		invocation = current
		return nil
	})
	return invocation, err
}

func resolveNormalizedPath(
	call toolcontracts.NormalizedToolCall,
	workspacePath string,
) (toolcontracts.NormalizedToolCall, error) {
	if call.Path == "" {
		return call, nil
	}
	if workspacePath == "" {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool workspace path is required")
	}
	path := call.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspacePath, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil || filepath.Clean(absolute) == "." {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool path is invalid")
	}
	call.Path = filepath.Clean(absolute)
	arguments := make(map[string]json.RawMessage)
	if err := json.Unmarshal(call.NormalizedArguments, &arguments); err != nil {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool path arguments are invalid")
	}
	encodedPath, err := json.Marshal(call.Path)
	if err != nil {
		return toolcontracts.NormalizedToolCall{}, err
	}
	arguments["path"] = encodedPath
	call.NormalizedArguments, err = json.Marshal(arguments)
	if err != nil {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool path arguments could not be encoded")
	}
	return call, nil
}
