package loop

import (
	"bytes"
	"context"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

func appendToolResult(
	ctx context.Context,
	messageWriter repository.ToolResultWriter,
	requestID identity.RequestID,
	executionID identity.AgentExecutionID,
	turnID identity.TurnID,
	callIndex int,
	block appcontext.ContextBlock,
) error {
	return appendTaskMessage(ctx, messageWriter, messagemodel.Message{
		ID:          "tool-result:" + executionID.String() + ":" + block.CallID,
		RequestID:   requestID.String(),
		ExecutionID: executionID.String(),
		TurnID:      turnID.String(),
		Role:        messagemodel.RoleTool,
		AuthorKind:  messagemodel.AuthorTool,
		AuthorID:    block.Name,
	}, []appcontext.ContextBlock{block})
}

// append执行输入Message 只接收已构造的消息归属与内容，不读取任务快照。
func appendTaskMessage(
	ctx context.Context,
	messageWriter repository.ToolResultWriter,
	value messagemodel.Message,
	blocks []appcontext.ContextBlock,
) error {
	value = outputMessage(value, blocks)
	return messageWriter.AppendToolResult(context.WithoutCancel(ctx), value)
}

func outputMessage(value messagemodel.Message, blocks []appcontext.ContextBlock) messagemodel.Message {
	value.Blocks = make([]messagemodel.Block, len(blocks))
	for index, block := range blocks {
		value.Blocks[index] = messagemodel.Block{
			Kind:           messagemodel.BlockKind(block.Kind),
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
