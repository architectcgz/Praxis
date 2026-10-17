package task

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	toolmodel "praxis/internal/core/tool_execution"
	toolcontracts "praxis/internal/tools/contracts"
)

// ReconcileToolResults 只在启动、恢复任何执行之前调用。
// 所有 running 调用均来自上次进程，其副作用未知，必须结算为 unknown 后才能恢复 Loop。
func (s *Service) ReconcileToolResults(ctx context.Context) error {
	return s.reconcileToolResults(ctx, "", "", true)
}

// 普通业务恢复不触碰当前仍在执行或等待审批的调用；Repository 查询本身不执行恢复。
func (s *Service) reconcileToolResults(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID, startup bool) error {
	missing, err := s.messages.ListMissingToolResults(ctx)
	if err != nil {
		return err
	}
	for _, fact := range missing {
		if sessionID != "" && fact.SessionID != sessionID || agentID != "" && fact.AgentID != agentID {
			continue
		}
		if err := s.tx.InTx(ctx, func(txCtx context.Context) error {
			task, err := s.tasks.Get(txCtx, fact.TaskID)
			if errors.Is(err, contracts.ErrNotFound) {
				return nil
			} // 快照之后 Session 可能已删除。
			if err != nil {
				return err
			}
			invocation, err := s.toolExecutions.FindByTaskCall(txCtx, fact.TaskID, fact.CallID)
			if err != nil {
				return err
			}
			at := s.clock.Now()
			switch invocation.Status {
			case toolmodel.StatusRequested, toolmodel.StatusPrepared, toolmodel.StatusReviewing, toolmodel.StatusApproved, toolmodel.StatusAwaitingApproval:
				if task.Active() {
					return nil
				}
				result := toolcontracts.NewToolError(string(toolmodel.FailureInterrupted), "The tool call was interrupted before execution.")
				err = invocation.Interrupt(toolmodel.Result{InlineContent: result.Payload, ErrorCode: toolmodel.FailureInterrupted}, at)
			case toolmodel.StatusRunning:
				if task.Active() && !startup {
					return nil
				}
				result := toolcontracts.NewToolError(string(toolmodel.FailureResultUnknown), "Execution stopped after the tool started. Its side effects are unknown; do not automatically retry.")
				err = invocation.MarkResultUnknown(toolmodel.Result{InlineContent: result.Payload, ErrorCode: toolmodel.FailureResultUnknown}, at)
			}
			if err != nil {
				return err
			}
			if !invocation.Status.Terminal() {
				return nil
			}
			if err := s.toolExecutions.Save(txCtx, invocation); err != nil {
				return err
			}
			writer, err := s.messages.Writer(txCtx, fact.SessionID, fact.AgentID)
			if err != nil {
				return err
			}
			payload := invocation.Result.InlineContent
			if payload == "" {
				payload = "(empty tool result)"
			}
			return writer.AppendToolResult(txCtx, sessionmodel.MessageData{
				ID:         "tool-result:" + fact.TaskID.String() + ":" + fact.CallID,
				RequestID:  fact.RequestID,
				TaskID:     fact.TaskID.String(),
				TurnID:     fact.TurnID.String(),
				Role:       sessionmodel.RoleTool,
				AuthorKind: sessionmodel.AuthorTool,
				AuthorID:   fact.Name,
				Blocks: []sessionmodel.Block{{
					Kind:    sessionmodel.BlockToolResult,
					Text:    payload,
					CallID:  fact.CallID,
					Name:    fact.Name,
					IsError: invocation.Result.ErrorCode != "",
				}},
			})
		}); err != nil {
			return err
		}
	}
	return nil
}
