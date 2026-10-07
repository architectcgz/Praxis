package toolinvocation

import (
	"context"
	"encoding/json"
	"errors"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	toolcontracts "praxis/internal/tools/contracts"
)

// RecordAssistant 在同一个 Session 事务内保存消息和整批 requested 调用。
// 只登记合法 JSON 的原始调用意图，不执行工具；参数语义和权限在 Invoke 中校验。
// recorder 必须使用同一存储的事务 Context；任一保存失败会回滚整批，重试复用调用身份。
func (s *Service) RecordAssistant(
	ctx context.Context,
	recorder agentruntime.MessageRecorder,
	message sessionmodel.MessageData,
	metadata agentruntime.ToolInvocationMetadata,
) error {
	if ctx == nil || recorder == nil || message.Role != sessionmodel.RoleAssistant ||
		message.AuthorKind != sessionmodel.AuthorAgent || message.AuthorID != metadata.AgentID.String() ||
		message.TaskID != metadata.TaskID.String() || metadata.TurnID == "" {
		return errors.New("assistant tool call ownership is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		if _, err := s.loadModelContext(txCtx, metadata); err != nil {
			return err
		}
		task, err := s.tasks.Get(txCtx, metadata.TaskID)
		if err != nil {
			return err
		}
		if message.RequestID != task.RequestID.String() {
			return contracts.ErrRequestConflict
		}
		if _, err := recorder.Append(txCtx, message); err != nil {
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
			if err := s.recordCall(txCtx, toolCall(block), metadata); err != nil {
				return err
			}
		}
		return nil
	})
}

func toolCall(block sessionmodel.Block) toolcontracts.ToolCall {
	arguments := block.Input
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	return toolcontracts.ToolCall{
		ID:        block.CallID,
		Name:      contracts.ToolName(block.Name),
		Arguments: arguments,
	}
}
