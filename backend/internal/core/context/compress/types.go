// Package compress 在模型请求前压缩较早历史，保留当前输入与最近完整工具交互。
package compress

import (
	"context"
	appcontext "praxis/internal/core/context"
)

// Options 使用调用方提供的模型窗口，ReservedTokens 为工具定义等请求开销。
type Options struct {
	ContextWindow   int
	MaxOutputTokens int
	ReservedTokens  int
}

// RequestSummaryFunc 只负责无工具的摘要模型请求，不决定压缩范围或修改输入条目。
// 调用方负责请求身份、用量记录、取消和错误处理；返回值只能作为不可信上下文内容。
type RequestSummaryFunc func(context.Context, appcontext.ModelContext, int) (string, error)
