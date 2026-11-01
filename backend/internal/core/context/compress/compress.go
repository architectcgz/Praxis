package compress

import (
	"context"
	"encoding/json"
	"errors"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// Compress 只在完整用户轮次结束后将整个 Context 合成为摘要，不裁剪原始 Entry。
// 失败时调用方继续持有原 Context；摘要成功提交后才切换引用列表。
func Compress(ctx context.Context, value appcontext.ModelContext, options Options, requestSummary RequestSummaryFunc) (appcontext.ModelContext, bool, error) {
	if err := ctx.Err(); err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if options.ContextWindow <= 0 || options.MaxOutputTokens <= 0 || options.ReservedTokens < 0 || options.MaxOutputTokens >= options.ContextWindow || options.ReservedTokens >= options.ContextWindow-options.MaxOutputTokens {
		return appcontext.ModelContext{}, false, errors.New("context compression limits are invalid")
	}
	estimated, err := value.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if !appcontext.NearContextLimit(estimated+options.ReservedTokens, options.MaxOutputTokens, options.ContextWindow) {
		return value, false, nil
	}
	if requestSummary == nil {
		return appcontext.ModelContext{}, false, errors.New("context summary requester is required")
	}
	encoded, err := json.Marshal(summaryEntries(value.Entries))
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	budget := options.ContextWindow - options.MaxOutputTokens - options.ReservedTokens
	limit := min(maxSummaryTokens, options.MaxOutputTokens, max(1, percent(budget, compressionTargetPercent)))
	text, err := summarizeTranscript(ctx, encoded, options.ContextWindow, limit, options.SummaryFormat, requestSummary)
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	compressed := appcontext.ModelContext{
		SystemPrompt: value.SystemPrompt,
		Entries: []sessionentry.ContextEntry{{
			Summary:    true,
			Role:       role.User,
			AuthorKind: sessionentry.AuthorSystem,
			Content: []sessionentry.Block{{
				Kind: sessionentry.BlockText,
				Text: options.SummaryFormat.ResultPrefix + text,
			}},
		}},
	}
	after, err := compressed.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if after >= estimated || appcontext.NearContextLimit(after+options.ReservedTokens, options.MaxOutputTokens, options.ContextWindow) {
		return appcontext.ModelContext{}, false, errors.New("context summary did not sufficiently reduce input")
	}
	return compressed, true, nil
}
