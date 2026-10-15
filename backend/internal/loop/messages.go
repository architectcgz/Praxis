package loop

import (
	"bytes"
	"context"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
)

func appendToolResult(
	ctx context.Context,
	messageRecorder MessageRecorder,
	requestID contracts.RequestID,
	taskID contracts.TaskID,
	turnID contracts.TurnID,
	callIndex int,
	block appcontext.ContextBlock,
) error {
	return appendTaskMessage(ctx, messageRecorder, sessionmodel.MessageData{
		ID:         "tool-result:" + taskID.String() + ":" + block.CallID,
		RequestID:  requestID.String(),
		TaskID:     taskID.String(),
		TurnID:     turnID.String(),
		Role:       sessionmodel.RoleTool,
		AuthorKind: sessionmodel.AuthorTool,
		AuthorID:   block.Name,
	}, []appcontext.ContextBlock{block})
}

// appendTaskMessage 只接收已构造的消息归属与内容，不读取任务快照。
func appendTaskMessage(
	ctx context.Context,
	messageRecorder MessageRecorder,
	value sessionmodel.MessageData,
	blocks []appcontext.ContextBlock,
) error {
	value = taskMessage(value, blocks)
	_, err := messageRecorder.Append(context.WithoutCancel(ctx), value)
	return err
}

func taskMessage(value sessionmodel.MessageData, blocks []appcontext.ContextBlock) sessionmodel.MessageData {
	value.Blocks = make([]sessionmodel.Block, len(blocks))
	for index, block := range blocks {
		value.Blocks[index] = sessionmodel.Block{
			Kind:           sessionmodel.BlockKind(block.Kind),
			Text:           block.Text,
			CallID:         block.CallID,
			Name:           block.Name,
			Input:          bytes.Clone(block.Input),
			IsError:        block.IsError,
			ReasoningState: block.ReasoningState.Clone(),
		}
	}
	return value
}

// MessageRecorder 只写当前 Agent 执行产生的持久化消息（会话历史不回读）。
type MessageRecorder interface {
	Append(context.Context, sessionmodel.MessageData) (sessionmodel.MessageData, error)
	AppendAssistant(context.Context, sessionmodel.MessageData, contracts.ToolInvocationContext) error
}
