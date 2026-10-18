package loop

import (
	"bytes"
	"context"

	appcontext "praxis/internal/core/context"
	"praxis/internal/core/identity"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/repository"
)

func appendToolResult(
	ctx context.Context,
	messageWriter repository.MessageWriter,
	requestID identity.RequestID,
	taskID identity.TaskID,
	turnID identity.TurnID,
	callIndex int,
	block appcontext.ContextBlock,
) error {
	return appendTaskMessage(ctx, messageWriter, sessionmodel.MessageData{
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
	messageWriter repository.MessageWriter,
	value sessionmodel.MessageData,
	blocks []appcontext.ContextBlock,
) error {
	value = taskMessage(value, blocks)
	return messageWriter.AppendToolResult(context.WithoutCancel(ctx), value)
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
