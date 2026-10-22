package context

import sessionentry "praxis/internal/core/session/entry"

// Append 沿用唯一 Entry 类型；模型只选择非展示性思考内容，不改动历史。
func (c ModelContext) Append(value sessionentry.ContextEntry) ModelContext {
	content := make([]sessionentry.Block, 0, len(value.Content))
	for _, block := range value.Content {
		if block.Kind != sessionentry.BlockThinking {
			content = append(content, block)
		}
	}
	if len(content) == 0 {
		return c
	}
	value.Content = content
	c.Entries = append(c.Entries, value)
	return c
}

func NewToolResultBlock(callID, name, payload string, isError bool) sessionentry.Block {
	return sessionentry.Block{Kind: sessionentry.BlockToolResult, CallID: callID, Name: name, Text: payload, IsError: isError}
}
