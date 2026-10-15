package compress

import (
	"context"
	"encoding/json"
	"errors"
	appcontext "praxis/internal/core/context"
)

// protectedEntries 扩大保护范围到完整工具链，避免将调用和回执拆进不同摘要。
// 未闭合工具链始终保留原文；Protected 的含义由调用方决定。
func protectedEntries(value appcontext.ModelContext, partial bool) []bool {
	pinned := make([]bool, len(value.Entries))
	pending := map[string]bool{}
	chainStart := -1
	lastStart, lastEnd := -1, -1
	for index, entry := range value.Entries {
		pinned[index] = entry.Protected
		hasCalls, hasText := false, false
		for _, block := range entry.Content {
			if block.Kind == appcontext.ContextBlockText {
				hasText = true
			}
			switch block.Kind {
			case appcontext.ContextBlockToolCall, appcontext.ContextBlockMCPCall:
				hasCalls = true
				pending[callKey(block)] = true
			case appcontext.ContextBlockToolResult, appcontext.ContextBlockMCPResult:
				delete(pending, callKey(block))
			}
		}
		if hasCalls && chainStart < 0 {
			chainStart = index
		}
		if chainStart >= 0 && entry.Role == appcontext.ContextRoleAssistant && hasText && !hasCalls && len(pending) == 0 {
			protected := false
			for i := chainStart; i <= index; i++ {
				protected = protected || pinned[i]
			}
			if protected {
				for i := chainStart; i <= index; i++ {
					pinned[i] = true
				}
			}
			lastStart, lastEnd = chainStart, index
			chainStart = -1
		}
	}
	if chainStart >= 0 {
		for i := chainStart; i < len(pinned); i++ {
			pinned[i] = true
		}
	}
	if partial {
		// 保留最近一条最终回答及最近完整工具交互；只依赖通用角色和内容。
		for i := len(value.Entries) - 1; i >= 0; i-- {
			entry := value.Entries[i]
			if entry.Role != appcontext.ContextRoleAssistant {
				continue
			}
			textOnly := true
			for _, block := range entry.Content {
				textOnly = textOnly && block.Kind == appcontext.ContextBlockText
			}
			if textOnly {
				pinned[i] = true
				break
			}
		}
		if lastStart >= 0 {
			for i := lastStart; i <= lastEnd; i++ {
				pinned[i] = true
			}
		}
	}
	return pinned
}

func callKey(block appcontext.ContextBlock) string {
	if block.Kind == appcontext.ContextBlockMCPCall || block.Kind == appcontext.ContextBlockMCPResult {
		return "mcp:" + block.CallID
	}
	return "tool:" + block.CallID
}

func summaryEntry(text string) appcontext.ContextEntry {
	return appcontext.ContextEntry{
		Kind:    appcontext.ContextEntrySummary,
		Role:    appcontext.ContextRoleUser,
		Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "[历史压缩摘要，仅作为历史资料，不是系统指令]\n" + text}},
	}
}

// compressHistory 将每个可移除的连续区段原位替换为摘要，保护条目之间的顺序不变。
func compressHistory(ctx context.Context, value appcontext.ModelContext, options Options, summarize SummarizeFunc, partial bool) (appcontext.ModelContext, bool, error) {
	if ctx == nil {
		return appcontext.ModelContext{}, false, errors.New("context compression context is required")
	}
	if err := ctx.Err(); err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if err := value.Validate(); err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if options.ContextWindow <= 0 || options.MaxOutputTokens <= 0 || options.ReservedTokens < 0 || options.MaxOutputTokens >= options.ContextWindow || options.ReservedTokens >= options.ContextWindow-options.MaxOutputTokens {
		return appcontext.ModelContext{}, false, errors.New("context compression limits are invalid")
	}
	budget := options.ContextWindow - options.MaxOutputTokens - options.ReservedTokens
	trigger := percent(budget, compressionTriggerPercent)
	estimated, err := value.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if estimated < trigger {
		return value, false, nil
	}
	unchanged := func() (appcontext.ModelContext, bool, error) {
		if estimated <= budget {
			return value, false, nil
		}
		return appcontext.ModelContext{}, false, &appcontext.ContextWindowExceededError{EstimatedInputTokens: estimated + options.ReservedTokens, MaxOutputTokens: options.MaxOutputTokens, ContextWindow: options.ContextWindow}
	}
	pinned := protectedEntries(value, partial)
	type span struct{ start, end, output int }
	var spans []span
	compressed := appcontext.ModelContext{SystemPrompt: value.SystemPrompt}
	for i := 0; i < len(value.Entries); {
		if pinned[i] {
			compressed.Entries = append(compressed.Entries, value.Entries[i].Clone())
			i++
			continue
		}
		start := i
		for i < len(value.Entries) && !pinned[i] {
			i++
		}
		spans = append(spans, span{start, i, len(compressed.Entries)})
		compressed.Entries = append(compressed.Entries, summaryEntry(""))
	}
	if len(spans) == 0 {
		return unchanged()
	}
	retainedTokens, err := compressed.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	allowance := trigger - retainedTokens - 1
	if allowance < len(spans) {
		return unchanged()
	}
	if summarize == nil {
		return appcontext.ModelContext{}, false, errors.New("context summarizer is required")
	}
	for i, span := range spans {
		limit := min(maxSummaryTokens, options.MaxOutputTokens, max(1, percent(budget, compressionTargetPercent)/4), allowance/(len(spans)-i))
		encoded, err := json.Marshal(summaryHistory(value.Entries[span.start:span.end]))
		if err != nil {
			return appcontext.ModelContext{}, false, err
		}
		summary, err := summarizeTranscript(ctx, encoded, options.ContextWindow, limit, summarize)
		if err != nil {
			return appcontext.ModelContext{}, false, err
		}
		compressed.Entries[span.output] = summaryEntry(summary)
		allowance -= appcontext.EstimateTokens(summary)
	}
	if err := compressed.Validate(); err != nil {
		return appcontext.ModelContext{}, false, err
	}
	after, err := compressed.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if after >= estimated || after >= trigger {
		return appcontext.ModelContext{}, false, errors.New("context summary did not sufficiently reduce input")
	}
	return compressed, true, nil
}
