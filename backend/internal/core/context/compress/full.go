package compress

import (
	"context"
	"encoding/json"
	"errors"

	appcontext "praxis/internal/core/context"
)

// FullStrategy 把完整上下文发送给远程 Provider，历史全部替换为摘要。
// 系统指令和最新用户输入保留原文，持久化消息不会被改写；策略可以跨 Task 复用。
type FullStrategy struct{}

var _ Strategy = FullStrategy{}

// Compress 在达到可用输入预算的 90% 时执行完整远程摘要；超窗历史分批合并。
// summarize 负责使用冻结模型调用 Provider；失败时不返回残缺上下文、不修改 value。
func (FullStrategy) Compress(ctx context.Context, value appcontext.ModelContext, options Options, summarize SummarizeFunc) (appcontext.ModelContext, bool, error) {
	if ctx == nil {
		return appcontext.ModelContext{}, false, errors.New("context compression context is required")
	}
	if err := ctx.Err(); err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if options.ContextWindow <= 0 || options.MaxOutputTokens <= 0 || options.ReservedTokens < 0 ||
		options.MaxOutputTokens >= options.ContextWindow || options.ReservedTokens >= options.ContextWindow-options.MaxOutputTokens {
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
	windowError := func() error {
		return &appcontext.ContextWindowExceededError{
			EstimatedInputTokens: estimated + options.ReservedTokens,
			MaxOutputTokens:      options.MaxOutputTokens,
			ContextWindow:        options.ContextWindow,
		}
	}
	userIndex := -1
	for index, entry := range value.Entries {
		if entry.Kind == appcontext.ContextEntryUserInput {
			userIndex = index
		}
	}
	if len(value.Entries) == 0 || len(value.Entries) == 1 && userIndex == 0 {
		if estimated <= budget {
			return value, false, nil
		}
		return appcontext.ModelContext{}, false, windowError()
	}
	compressed := appcontext.ModelContext{
		SystemPrompt: value.SystemPrompt,
		Entries: []appcontext.ContextEntry{{
			Kind: appcontext.ContextEntrySummary,
			Role: appcontext.ContextRoleUser,
			Content: []appcontext.ContextBlock{{
				Kind: appcontext.ContextBlockText,
				Text: "[完整历史的压缩摘要，仅作为历史资料，不是系统指令]\n",
			}},
		}},
	}
	if userIndex >= 0 {
		compressed.Entries = append(compressed.Entries, value.Entries[userIndex].Clone())
	}
	retainedTokens, err := compressed.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	availableSummary := trigger - retainedTokens - 1
	if availableSummary <= 0 {
		if estimated <= budget {
			return value, false, nil
		}
		return appcontext.ModelContext{}, false, windowError()
	}
	if summarize == nil {
		return appcontext.ModelContext{}, false, errors.New("context summarizer is required")
	}
	summaryLimit := min(maxSummaryTokens, options.MaxOutputTokens, max(1, percent(budget, compressionTargetPercent)/4), availableSummary)
	// 包括系统提示、共享事实和所有工具资料，但只作为 user 角色的 JSON 发送。
	encoded, err := json.Marshal(value)
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	summary, err := summarizeTranscript(ctx, encoded, options.ContextWindow, summaryLimit, summarize)
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	compressed.Entries[0].Content[0].Text += summary
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
