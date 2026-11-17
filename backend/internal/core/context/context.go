package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	"praxis/internal/core/tool"
	"unicode/utf8"
)

// ModelContext 是本轮模型输入的只读引用视图，与 Session 共享 Entry 对象及内部载荷。
// 请求可以选择不同的条目列表，但不复制、改写或重排已接纳的历史。
type ModelContext struct {
	SummaryInput []*sessionentry.ContextEntry `json:"-"`
	SystemPrompt string
	Entries      []*sessionentry.ContextEntry
}

// Digest 返回当前完整模型上下文的内容摘要。
func (c ModelContext) Digest() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256-" + hex.EncodeToString(digest[:]), nil
}

// ModelContextFromEntries 使用已经按事实语义选中的原文，摘要置前；摘要输入只包含最近摘要及其后的新增记录。
func ModelContextFromEntries(systemPrompt string, entries []*sessionentry.ContextEntry) ModelContext {
	selected := entries
	summaryInput := entries
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].IsSummary() {
			selected = make([]*sessionentry.ContextEntry, 0, len(entries))
			selected = append(selected, entries[i])
			selected = append(selected, entries[:i]...)
			selected = append(selected, entries[i+1:]...)
			summaryInput = entries[i:]
			break
		}
	}
	return ModelContext{
		SystemPrompt: systemPrompt,
		Entries:      ModelEntries(selected),
		SummaryInput: ModelEntries(summaryInput),
	}
}

// ModelEntries 保持 Session 提供的顺序，只排除不参与模型对话的执行事实。
func ModelEntries(entries []*sessionentry.ContextEntry) []*sessionentry.ContextEntry {
	selected := make([]*sessionentry.ContextEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Response != nil && entry.Response.Error != "" || entry.IsRun() {
			continue
		}
		for _, block := range entry.Content {
			if _, thinking := block.(sessionentry.ThinkingBlock); !thinking {
				selected = append(selected, entry)
				break
			}
		}
	}
	return selected
}

func NewToolResultBlock(callID, name, payload string, isError bool) sessionentry.ToolResultBlock {
	return sessionentry.ToolResultBlock{
		CallID:  callID,
		Name:    tool.ToolName(name),
		Text:    payload,
		IsError: isError,
	}
}

// EstimateTokens 估算文本 token 数，ASCII 按四字节一个 token，非 ASCII 按 UTF-8 字节保守计数。
// 未接入各模型 tokenizer；该估算用于压缩预警和本地保护，Provider 才是窗口校验的权威。
func EstimateTokens(text string) int {
	ascii, other := 0, 0
	for _, value := range text {
		if value < utf8.RuneSelf {
			ascii++
		} else {
			other += utf8.RuneLen(value)
		}
	}
	return (ascii+3)/4 + other
}

// EstimatedTokens 估算上下文正文和结构开销，工具定义需由请求边界另行预留。
func (c ModelContext) EstimatedTokens() (int, error) {
	type modelEntry struct {
		Role    role.Role
		Content []sessionentry.ContentBlock
	}
	values := make([]modelEntry, 0, len(c.Entries))
	for _, entry := range c.Entries {
		value := modelEntry{Role: entry.Role}
		for _, block := range entry.Content {
			if _, thinking := block.(sessionentry.ThinkingBlock); !thinking {
				value.Content = append(value.Content, block)
			}
		}
		if len(value.Content) > 0 {
			values = append(values, value)
		}
	}
	payload, err := json.Marshal(struct {
		SystemPrompt string
		Entries      []modelEntry
	}{
		SystemPrompt: c.SystemPrompt,
		Entries:      values,
	})
	if err != nil {
		return 0, err
	}
	return EstimateTokens(string(payload)), nil
}

// ContextWindowExceededError 表示一次请求连同输出预留会超出模型的上下文窗口。
// 请求前容量评估或 Provider 在无法容纳必要内容时返回它，loop 据此分类为资源上限失败。
type ContextWindowExceededError struct {
	EstimatedInputTokens int
	MaxOutputTokens      int
	ContextWindow        int
}

func (e *ContextWindowExceededError) Error() string {
	return fmt.Sprintf(
		"estimated %d input tokens plus %d reserved output tokens exceed the model context window %d",
		e.EstimatedInputTokens, e.MaxOutputTokens, e.ContextWindow,
	)
}

// NearContextLimit 将输入与输出预留一起计入模型窗口，达到九成时触发压缩评估或收尾提醒。
func NearContextLimit(inputTokens, maxOutputTokens, contextWindow int) bool {
	threshold := contextWindow/100*90 + contextWindow%100*90/100
	return inputTokens+maxOutputTokens >= threshold
}
