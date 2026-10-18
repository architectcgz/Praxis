package context

import (
	"slices"
)

// AppendProviderOutput 追加已校验的 Provider 输出，返回更新后的模型上下文。
// 思考内容只用于展示，不进入后续模型请求；其余 block 复制后追加，全为思考内容时不追加条目。
func (c ModelContext) AppendProviderOutput(blocks []ContextBlock) ModelContext {
	content := make([]ContextBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Kind != ContextBlockThinking {
			content = append(content, block.Clone())
		}
	}
	if len(content) == 0 {
		return c
	}
	c.Entries = append(c.Entries, ContextEntry{
		Kind:    ContextEntryProviderOutput,
		Role:    ContextRoleAssistant,
		Content: content,
	})
	return c
}

// NewToolResultBlock 把已规范化的调用身份、结果正文和错误标识构造为上下文内容块。
// 保留原始正文，包括空结果；工具执行和错误分类由调用方负责。
func NewToolResultBlock(callID, name, payload string, isError bool) ContextBlock {
	return ContextBlock{
		Kind:    ContextBlockToolResult,
		Text:    payload,
		CallID:  callID,
		Name:    name,
		IsError: isError,
	}
}

// AppendToolResult 复制并追加已构造的工具结果 block，返回更新后的模型上下文。
// 调用方负责在追加前持久化结果回执，避免下一轮请求读取未落库的结果。
func (c ModelContext) AppendToolResult(block ContextBlock) ModelContext {
	c.Entries = append(c.Entries, ContextEntry{
		Kind:    ContextEntryToolResult,
		Role:    ContextRoleTool,
		Content: []ContextBlock{block.Clone()},
	})
	return c
}

// RestoreToolResult 按最近匹配调用中的声明顺序补齐回执，不改写已有内容。
// 恢复时其他调用的回执可能已经落库，不能把缺失项统一追加到它们之后。
func (c ModelContext) RestoreToolResult(block ContextBlock) ModelContext {
	for index := len(c.Entries) - 1; index >= 0; index-- {
		var calls []ContextBlock
		ordinal := -1
		for _, candidate := range c.Entries[index].Content {
			if candidate.Kind != ContextBlockToolCall {
				continue
			}
			if candidate.CallID == block.CallID {
				ordinal = len(calls)
			}
			calls = append(calls, candidate)
		}
		if ordinal < 0 {
			continue
		}
		insert := index + 1
		for insert < len(c.Entries) && c.Entries[insert].Role == ContextRoleTool {
			resultOrder := len(calls)
			for _, result := range c.Entries[insert].Content {
				if result.CallID == block.CallID {
					return c
				}
				for order, call := range calls {
					if call.CallID == result.CallID {
						resultOrder = min(resultOrder, order)
					}
				}
			}
			if resultOrder > ordinal {
				break
			}
			insert++
		}
		entry := ContextEntry{Kind: ContextEntryToolResult, Role: ContextRoleTool, Content: []ContextBlock{block.Clone()}}
		c.Entries = slices.Insert(c.Entries, insert, entry)
		return c
	}
	return c.AppendToolResult(block)
}
