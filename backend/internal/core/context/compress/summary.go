package compress

import (
	"context"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
)

const (
	compressionTriggerPercent = 90
	compressionTargetPercent  = 60
	maxSummaryTokens          = 4096
)

func summarizeTranscript(ctx context.Context, encoded []byte, contextWindow, summaryLimit int, format SummaryFormat, summarize SummarizeFunc) (string, error) {
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
			part := summaryContext(format, summary, string(remaining[:middle]))
			tokens, err := part.EstimatedTokens()
			if err != nil {
				return "", err
			}
			if tokens <= percent(contextWindow-summaryLimit, compressionTriggerPercent) {
				low = middle
			} else {
				high = middle - 1
			}
		}
		if low == 0 {
			tokens, err := summaryContext(format, summary, string(remaining[:1])).EstimatedTokens()
			if err != nil {
				return "", err
			}
			return "", &appcontext.ContextWindowExceededError{
				EstimatedInputTokens: tokens,
				MaxOutputTokens:      summaryLimit,
				ContextWindow:        contextWindow,
			}
		}
		text, err := summarize(ctx, summaryContext(format, summary, string(remaining[:low])), summaryLimit)
		if err != nil {
			return "", fmt.Errorf("summarize context: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return "", err
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

func summaryContext(format SummaryFormat, previous, transcript string) appcontext.ModelContext {
	return appcontext.ModelContext{
		SystemPrompt: format.SystemPrompt,
		Entries: []sessionentry.ContextEntry{{
			Kind: sessionentry.ContextEntryUserInput,
			Role: sessionentry.RoleUser,
			Content: []sessionentry.Block{{
				Kind: sessionentry.BlockText,
				Text: format.PreviousSummaryHeader + previous + format.TranscriptHeader + transcript,
			}},
		}},
	}
}
