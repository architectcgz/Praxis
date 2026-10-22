package model

import (
	"bytes"
	sessionentry "praxis/internal/core/session/entry"
	"strings"
)

// EntryBlocksFromEvents 把模型流事件转换为有序上下文 block。
// events 来自流消费阶段收集的有序内容和推理状态；参数与推理状态载荷均深拷贝。
// 空白文本不进入上下文，但模型输出预算仍由流消费阶段按原始文本统计。
func EntryBlocksFromEvents(events []ModelStreamEvent) []sessionentry.Block {
	blocks := make([]sessionentry.Block, 0, len(events))
	for _, event := range events {
		if event.Kind == StreamReasoningState {
			blocks = append(blocks, sessionentry.Block{
				Kind:           sessionentry.BlockReasoningState,
				ReasoningState: event.ReasoningState.Clone(),
			})
			continue
		}
		if event.Kind == StreamToolCall {
			call := event.ToolCall
			blocks = append(blocks, sessionentry.Block{
				Kind:   sessionentry.BlockToolCall,
				CallID: call.ID,
				Name:   string(call.Name),
				Input:  bytes.Clone(call.Arguments),
			})
			continue
		}
		if (event.Kind != StreamTextDelta && event.Kind != StreamThinkingDelta) || strings.TrimSpace(event.Text) == "" {
			continue
		}
		kind := sessionentry.BlockText
		if event.Kind == StreamThinkingDelta {
			kind = sessionentry.BlockThinking
		}
		blocks = append(blocks, sessionentry.Block{Kind: kind, Text: event.Text})
	}
	return blocks
}
