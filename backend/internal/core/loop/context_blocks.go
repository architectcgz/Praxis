package loop

import (
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"strings"
)

// entryBlocksFromEvents 把模型流事件转换为有序上下文 block。
// events 来自流消费阶段收集的有序内容和推理状态；参数保留原文，推理状态载荷深拷贝。
// 空白文本不进入上下文，但模型输出预算仍由流消费阶段按原始文本统计。
func entryBlocksFromEvents(events []providerapi.Event) []sessionentry.ContentBlock {
	blocks := make([]sessionentry.ContentBlock, 0, len(events))
	for _, event := range events {
		if event.Kind == providerapi.StreamReasoningState {
			blocks = append(blocks, sessionentry.ReasoningStateBlock{
				State: event.ReasoningState.Clone(),
			})
			continue
		}
		if event.Kind == providerapi.StreamToolCall {
			call := event.ToolCall
			blocks = append(blocks, sessionentry.ToolCallBlock{
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
			continue
		}
		if (event.Kind != providerapi.StreamTextDelta && event.Kind != providerapi.StreamThinkingDelta) || strings.TrimSpace(event.Text) == "" {
			continue
		}
		if event.Kind == providerapi.StreamThinkingDelta {
			blocks = append(blocks, sessionentry.ThinkingBlock{Text: event.Text})
		} else {
			blocks = append(blocks, sessionentry.TextBlock{Text: event.Text})
		}
	}
	return blocks
}
