package compress

import (
	"context"
	appcontext "praxis/internal/core/context"
)

// PartialStrategy 通过通用保护标记和工具链边界选择可摘要内容，不解释业务来源。
type PartialStrategy struct{}

var _ Strategy = PartialStrategy{}

func (PartialStrategy) Compress(ctx context.Context, value appcontext.ModelContext, options Options, summarize SummarizeFunc) (appcontext.ModelContext, bool, error) {
	return compressHistory(ctx, value, options, summarize, true)
}
