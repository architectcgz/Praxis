package context

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// ContentFromEntries 从已选择的只读条目构建模型投影，保护标记只属于本轮请求。
// 执行输入的存在性已在准入时检查，完整执行压缩后上下文可以只包含摘要。
func ContentFromEntries(systemPrompt, inputEntryID string, entries []*sessionentry.ContextEntry) Content {
	content := Content{SystemPrompt: systemPrompt}
	for _, entry := range ModelEntries(sessionentry.Values(entries)) {
		if entry.ID == inputEntryID && entry.Role == role.User {
			entry.Protected = true
		}
		content.Entries = append(content.Entries, entry)
	}
	return content
}

// ContextBuilder 复制通用内容、丢弃展示用思考块并保持顺序，拒绝连必要原文都无法容纳的窗口。
// 输入内容的结构约束由调用方的输入边界保证，本包只做复制与容量检查。
// 此处不触发压缩，完整请求的窗口硬上限由 Provider 在编码后检查。
type ContextBuilder struct{}

func NewContextBuilder() ContextBuilder { return ContextBuilder{} }

// Build 返回去掉思考内容的模型上下文，并按 Protected 原文估算必要容量。
// 全部内容均为思考块的条目会被跳过；服务入口已保证当前输入存在模型可见内容，
// 因此调用方不会拿到空条目上下文，本方法不再单独检查。
func (ContextBuilder) Build(content Content, limits Limits) (ModelContext, error) {
	value := ModelContext{SystemPrompt: content.SystemPrompt}
	minimum := ModelContext{SystemPrompt: content.SystemPrompt}
	for _, entry := range content.Entries {
		copy, visible := modelVisibleEntry(entry)
		if !visible {
			continue
		}
		value.Entries = append(value.Entries, copy)
		if copy.Protected {
			minimum.Entries = append(minimum.Entries, copy)
		}
	}
	tokens, err := minimum.EstimatedTokens()
	if err != nil {
		return ModelContext{}, err
	}
	if tokens > limits.ContextWindow-limits.MaxOutputTokens {
		return ModelContext{}, &ContextWindowExceededError{EstimatedInputTokens: tokens, MaxOutputTokens: limits.MaxOutputTokens, ContextWindow: limits.ContextWindow}
	}
	return value, nil
}

// modelVisibleEntry 移除展示用思考；推理状态由 Provider 按目标模型决定是否重放。
func modelVisibleEntry(value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool) {
	content := make([]sessionentry.ContentBlock, 0, len(value.Content))
	for _, block := range value.Content {
		if _, thinking := block.(sessionentry.ThinkingBlock); !thinking {
			content = append(content, block)
		}
	}
	value.Content = content
	return value, len(content) > 0
}
