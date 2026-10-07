package model

import (
	"bytes"
	"strings"

	appcontext "praxis/internal/core/context"
)

// ContextBlocksFromEvents 把模型流事件转换为有序上下文 block。
// events 来自流消费阶段收集的文本、思考和工具调用事件；工具参数会复制，调用身份不再规范化。
// 空白文本不进入上下文，但模型输出预算仍由流消费阶段按原始文本统计。
func ContextBlocksFromEvents(events []ModelStreamEvent) []appcontext.ContextBlock {
	blocks := make([]appcontext.ContextBlock, 0, len(events))
	for _, event := range events {
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
		if strings.TrimSpace(event.Text) == "" {
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
