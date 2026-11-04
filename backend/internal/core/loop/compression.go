package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
)

// Compress 生成轮后摘要的上下文结果；Entry 构造、记录和 Run 更新由执行服务负责。
func (r *Runner) Compress(ctx context.Context, params RunParams, deps RunDependencies, value appcontext.ModelContext) (appcontext.ModelContext, bool, error) {
	reservedTokens, err := estimateRequestOverhead(r.Tools.List(), nil)
	if err != nil {
		return value, false, NewFailure(FailureContract, err)
	}
	return contextcompress.Compress(ctx, value, contextcompress.Options{
		ContextWindow:   params.Model.ContextWindow,
		MaxOutputTokens: params.Model.MaxOutputTokens,
		ReservedTokens:  reservedTokens,
	}, func(summaryCtx context.Context, summaryInput appcontext.ModelContext, limit int) (string, error) {
		return r.summarizeContext(summaryCtx, params, deps, summaryInput, limit)
	})
}
