package model

import (
	"bytes"
	"strings"

	appcontext "praxis/internal/core/context"
)

// ContextBlocksFromEvents 把模型流事件转换为有序上下文 block。
// events 来自流消费阶段收集的有序内容和推理状态；参数与推理状态载荷均深拷贝。
// 空白文本不进入上下文，但模型输出预算仍由流消费阶段按原始文本统计。
func ContextBlocksFromEvents(events []ModelStreamEvent) []appcontext.ContextBlock {
	blocks := make([]appcontext.ContextBlock, 0, len(events))
	for _, event := range events {
		if event.Kind == StreamReasoningState {
			blocks = append(blocks, appcontext.ContextBlock{
				Kind:           appcontext.ContextBlockReasoningState,
				ReasoningState: event.ReasoningState.Clone(),
			})
			continue
		}
		if event.Kind == StreamToolCall {
			call := event.ToolCall
			blocks = append(blocks, appcontext.ContextBlock{
				Kind:   appcontext.ContextBlockToolCall,
				CallID: call.ID,
				Name:   string(call.Name),
				Input:  bytes.Clone(call.Arguments),
			})
			continue
		}
		if (event.Kind != StreamTextDelta && event.Kind != StreamThinkingDelta) || strings.TrimSpace(event.Text) == "" {
			continue
		}
		kind := appcontext.ContextBlockText
		if event.Kind == StreamThinkingDelta {
			kind = appcontext.ContextBlockThinking
		}
		blocks = append(blocks, appcontext.ContextBlock{Kind: kind, Text: event.Text})
	}
	return blocks
}
