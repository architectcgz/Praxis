package execution

import (
	"context"
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// ReconcileToolResults 只在启动、恢复任何执行之前调用。
// 所有 running 调用均来自上次进程，其副作用未知，必须结算为 unknown 后才能恢复 Loop。
func (s *Service) ReconcileToolResults(ctx context.Context) error {
	return s.reconcileToolResults(ctx, "", "", true)
}

// 普通业务恢复不触碰当前仍在执行或等待审批的调用；Repository 查询本身不执行恢复。
func (s *Service) reconcileToolResults(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, startup bool) error {
	missing, err := s.messages.ListMissingToolResults(ctx)
	if err != nil {
		return err
	}
	for _, fact := range missing {
		if sessionID != "" && fact.SessionID != sessionID || agentID != "" && fact.AgentID != agentID {
			continue
		}
		if err := s.tx.InTx(ctx, func(txCtx context.Context) error {
			task, err := s.records.Get(txCtx, fact.ExecutionID)
			if errors.Is(err, fault.ErrNotFound) {
				return nil
			} // 快照之后 Session 可能已删除。
			if err != nil {
				return err
			}
			invocation, err := s.toolExecutions.FindByExecutionCall(txCtx, fact.ExecutionID, fact.CallID)
			if err != nil {
				return err
			}
			at := s.clock.Now()
			switch invocation.Status {
			case toolexecution.StatusRequested, toolexecution.StatusPrepared, toolexecution.StatusReviewing, toolexecution.StatusApproved, toolexecution.StatusAwaitingApproval:
				if task.Active() {
					return nil
				}
				result := tool.NewToolError(string(toolexecution.FailureInterrupted), "The tool call was interrupted before execution.")
				err = invocation.Interrupt(toolexecution.Result{InlineContent: result.Payload, ErrorCode: toolexecution.FailureInterrupted}, at)
			case toolexecution.StatusRunning:
				if task.Active() && !startup {
					return nil
				}
				result := tool.NewToolError(string(toolexecution.FailureResultUnknown), "Execution stopped after the tool started. Its side effects are unknown; do not automatically retry.")
				err = invocation.MarkResultUnknown(toolexecution.Result{InlineContent: result.Payload, ErrorCode: toolexecution.FailureResultUnknown}, at)
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
			return writer.AppendToolResult(txCtx, messagemodel.Message{
				ID:          "tool-result:" + fact.ExecutionID.String() + ":" + fact.CallID,
				RequestID:   fact.RequestID,
				ExecutionID: fact.ExecutionID.String(),
				TurnID:      fact.TurnID.String(),
				Role:        messagemodel.RoleTool,
				AuthorKind:  messagemodel.AuthorTool,
				AuthorID:    fact.Name,
				Blocks: []messagemodel.Block{{
					Kind:    messagemodel.BlockToolResult,
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
