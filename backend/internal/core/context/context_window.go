package context

import (
	"encoding/json"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	"unicode/utf8"
)

// EstimateTokens 估算文本 token 数，ASCII 按四字节一个 token，非 ASCII 按 UTF-8 字节保守计数。
// ponytail: 未接入各模型 tokenizer；该估算用于压缩预警和本地保护，Provider 才是窗口校验的权威。
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
	}{SystemPrompt: c.SystemPrompt, Entries: values})
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
