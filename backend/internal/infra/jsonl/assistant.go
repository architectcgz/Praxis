package jsonl

import (
	"context"
	"encoding/json"
	"errors"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/system"
)

// AppendAssistant 在同一个 Session 事务内保存消息和整批 requested 调用。
// 只登记合法 JSON 的原始调用意图，不执行工具；参数语义和权限在 Executor 中校验。
// 任一保存失败都回滚整批，重试复用同一调用身份。
func (w MessageWriter) AppendAssistant(
	ctx context.Context,
	message sessionmodel.MessageData,
	metadata contracts.ToolInvocationContext,
) error {
	if ctx == nil || w.owner.ID != metadata.AgentID || w.owner.SessionID != metadata.SessionID || message.Role != sessionmodel.RoleAssistant ||
		message.AuthorKind != sessionmodel.AuthorAgent || message.AuthorID != metadata.AgentID.String() ||
		message.TaskID != metadata.TaskID.String() || message.TurnID != metadata.TurnID.String() || metadata.TurnID == "" {
		return errors.New("assistant tool call ownership is required")
	}
	return w.streams.s.InTx(ctx, func(txCtx context.Context) error {
		task, err := (TaskRepository{w.streams.s}).Get(txCtx, metadata.TaskID)
		if err != nil {
			return err
		}
		if !task.Active() || task.SessionID != metadata.SessionID || task.AgentID != metadata.AgentID || task.Input.WorkspacePath != metadata.WorkspacePath || message.RequestID != task.RequestID.String() {
			return contracts.ErrRequestConflict
		}
		if _, err := w.Append(txCtx, message); err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, block := range message.Blocks {
			if block.Kind != sessionmodel.BlockToolCall {
				continue
			}
			if seen[block.CallID] {
				return contracts.ErrRequestConflict
			}
			seen[block.CallID] = true
			if err := w.recordCall(txCtx, toolCall(block), metadata); err != nil {
				return err
			}
		}
		if message.StopReason == "completed" || message.StopReason == "stop_sequence" {
			task.Progress.FinalStopReason = message.StopReason
			task.Progress.FinalAssistantID = message.ID
			return (TaskRepository{w.streams.s}).Save(txCtx, task)
		}
		return nil
	})
}

func toolCall(block sessionmodel.Block) contracts.ToolCall {
	arguments := block.Input
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	return contracts.ToolCall{
		ID:        block.CallID,
		Name:      contracts.ToolName(block.Name),
		Arguments: arguments,
	}
}

// recordCall 只在 assistant 保存事务中登记调用，重复身份必须保持原始意图和归属一致。
func (w MessageWriter) recordCall(
	ctx context.Context,
	call contracts.ToolCall,
	provided contracts.ToolInvocationContext,
) error {
	digest := toolmodel.ArgumentsDigest(call.Name, call.Arguments)
	if digest == "" {
		return errors.New("tool invocation arguments could not be fingerprinted")
	}
	existing, err := (ToolRepository{w.streams.s}).FindByTaskCall(ctx, provided.TaskID, call.ID)
	if err == nil {
		if existing.Name != call.Name || existing.ArgumentsDigest != digest || existing.TurnID != provided.TurnID ||
			existing.AgentID != provided.AgentID || existing.SessionID != provided.SessionID {
			return contracts.ErrRequestConflict
		}
		return nil
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		return err
	}
	invocation, err := toolmodel.NewToolInvocation(
		contracts.ToolInvocationID(system.IDsOrDefault(nil).New("toolinvocation")), provided.TaskID,
		provided.TurnID, provided.SessionID, provided.AgentID, call.ID, call.Name,
		call.Arguments, digest, system.ClockOrDefault(nil).Now(),
	)
	if err != nil {
		return err
	}
	return (ToolRepository{w.streams.s}).Save(ctx, invocation)
}
