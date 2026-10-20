package loop

import (
	"bytes"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
)

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
