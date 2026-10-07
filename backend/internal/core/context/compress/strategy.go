// Package compress 提供模型上下文压缩策略及其摘要调用契约。
package compress

import (
	"context"

	appcontext "praxis/internal/core/context"
)

// Strategy 决定是否压缩，以及哪些历史内容以摘要或原文保留。
// 实现不得修改调用方持有的上下文；摘要请求的执行、用量记录和取消由 summarize 负责。
type Strategy interface {
	// Compress 返回可发送的上下文和是否发生压缩；失败时不能返回供继续执行的残缺历史。
	Compress(context.Context, appcontext.ModelContext, Options, SummarizeFunc) (appcontext.ModelContext, bool, error)
}

// Options 使用当前 Task 的冻结窗口，ReservedTokens 为工具定义等请求开销。
type Options struct {
	ContextWindow   int
	MaxOutputTokens int
	ReservedTokens  int
}

// SummarizeFunc 对历史资料生成摘要。调用方负责无工具的模型请求、用量记录、取消和错误处理。
// 返回的摘要属于不可信历史数据，不能作为系统指令；失败时不得替换原上下文。
type SummarizeFunc func(context.Context, appcontext.ModelContext, int) (string, error)
