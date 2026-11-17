// Package compress 在模型请求前压缩较早历史，保留当前输入与最近完整工具交互。
package compress

import (
	"context"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
)

const (
	compressionTargetPercent = 60
	maxSummaryTokens         = 4096
)

// Options 使用调用方提供的模型窗口，ReservedTokens 为工具定义等请求开销。
type Options struct {
	InputTokens     int
	ContextWindow   int
	MaxOutputTokens int
	ReservedTokens  int
}

// RequestSummaryFunc 只负责无工具的摘要模型请求，不决定压缩范围或修改输入条目。
// 调用方负责请求身份、用量记录、取消和错误处理；返回值只能作为不可信上下文内容。
type RequestSummaryFunc func(context.Context, appcontext.ModelContext, int) (string, error)

// summaryEntry 只承载摘要模型需要的角色和正文，不复制会话条目的身份、计时及其他执行事实。
type summaryEntry struct {
	Role    role.Role                   `json:"role"`
	Content []sessionentry.ContentBlock `json:"content"`
}

// summaryEntries 不把签名、加密载荷或展示性思考正文发送给摘要模型。
func summaryEntries(entries []*sessionentry.ContextEntry) []summaryEntry {
	var selected []summaryEntry
	for _, entry := range entries {
		value := summaryEntry{Role: entry.Role}
		for _, block := range entry.Content {
			switch block.(type) {
			case sessionentry.ReasoningStateBlock, sessionentry.ThinkingBlock:
			default:
				value.Content = append(value.Content, block)
			}
		}
		if len(value.Content) > 0 {
			selected = append(selected, value)
		}
	}
	return selected
}

func summarizeTranscript(ctx context.Context, encoded []byte, contextWindow, summaryLimit int, requestSummary RequestSummaryFunc) (string, error) {
	remaining := []rune(string(encoded))
	summary := ""
	for len(remaining) > 0 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// 分片保持 UTF-8 完整；工具记录只作为 JSON 资料，不携带可执行调用。
		low, high := 0, len(remaining)
		for low < high {
			middle := low + (high-low+1)/2
			part := summaryContext(summary, string(remaining[:middle]))
			tokens, err := part.EstimatedTokens()
			if err != nil {
				return "", err
			}
			if !appcontext.NearContextLimit(tokens, summaryLimit, contextWindow) {
				low = middle
			} else {
				high = middle - 1
			}
		}
		if low == 0 {
			tokens, err := summaryContext(summary, string(remaining[:1])).EstimatedTokens()
			if err != nil {
				return "", err
			}
			return "", &appcontext.ContextWindowExceededError{
				EstimatedInputTokens: tokens,
				MaxOutputTokens:      summaryLimit,
				ContextWindow:        contextWindow,
			}
		}
		text, err := requestSummary(ctx, summaryContext(summary, string(remaining[:low])), summaryLimit)
		if err != nil {
			return "", fmt.Errorf("summarize context: %w", err)
		}
		// 调用边界提供已处理的摘要，算法只校验容量。
		summary = text
		if summary == "" || appcontext.EstimateTokens(summary) > summaryLimit {
			return "", errors.New("context summary is empty or exceeds its budget")
		}
		remaining = remaining[low:]
	}
	return summary, nil
}

func percent(value, ratio int) int {
	return value/100*ratio + value%100*ratio/100
}

func summaryContext(previous, transcript string) appcontext.ModelContext {
	return appcontext.ModelContext{
		SystemPrompt: summaryPrompt,
		Entries: []*sessionentry.ContextEntry{{
			Role: role.User,
			Content: []sessionentry.ContentBlock{
				sessionentry.TextBlock{Text: previousSummaryHeader + previous + transcriptHeader + transcript},
			},
		}},
	}
}
