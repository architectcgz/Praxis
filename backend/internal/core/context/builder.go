package context

import (
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// ContentFromEntries 从已维护的有效条目构建本轮模型内容，只为当前输入添加临时保护。
// 不扫描原始历史或重新应用摘要替代；当前输入没有可见正文时返回错误。
func ContentFromEntries(systemPrompt, inputEntryID string, entries []sessionentry.ContextEntry) (Content, error) {
	content := Content{SystemPrompt: systemPrompt}
	found := false
	for _, entry := range entries {
		value, visible := modelVisibleEntry(entry.Clone())
		if !visible {
			continue
		}
		if value.ID == inputEntryID && value.Role == role.User {
			value.Protected = true
			found = true
		}
		content.Entries = append(content.Entries, value)
	}
	if !found {
		return Content{}, errors.New("current input user entry is missing")
	}
	return content, nil
}

// ContextBuilder 复制通用内容、丢弃展示用思考块并保持顺序，拒绝连必要原文都无法容纳的窗口。
// 输入内容的结构约束由调用方的输入边界保证，本包只做复制与容量检查。
// 可压缩历史允许暂时超窗，由请求边界在调用模型前执行压缩。
type ContextBuilder struct{}

func NewContextBuilder() ContextBuilder { return ContextBuilder{} }

// Build 返回去掉思考内容的模型上下文，并按 Protected 原文估算必要容量。
// 全部内容均为思考块的条目会被跳过；服务入口已保证当前输入存在模型可见内容，
// 因此调用方不会拿到空条目上下文，本方法不再单独检查。
func (ContextBuilder) Build(content Content, limits Limits) (ModelContext, error) {
	value := ModelContext{SystemPrompt: content.SystemPrompt}
	minimum := ModelContext{SystemPrompt: content.SystemPrompt}
	for _, entry := range content.Entries {
		copy := entry
		copy.Content = nil
		for _, block := range entry.Content {
			if block.Kind != sessionentry.BlockThinking {
				copy.Content = append(copy.Content, block)
			}
		}
		if len(copy.Content) == 0 {
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
