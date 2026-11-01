// Package compress 在完整用户轮次结束后将当前上下文合成为一条摘要。
package compress

import (
	"context"
	appcontext "praxis/internal/core/context"
)

// SummaryFormat 由应用提供提示词和待摘要条目的文本格式。
type SummaryFormat struct {
	SystemPrompt          string
	PreviousSummaryHeader string
	TranscriptHeader      string
	ResultPrefix          string
}

// Options 使用调用方提供的模型窗口，ReservedTokens 为工具定义等请求开销。
type Options struct {
	SummaryFormat   SummaryFormat
	ContextWindow   int
	MaxOutputTokens int
	ReservedTokens  int
}

// RequestSummaryFunc 只负责无工具的摘要模型请求，不决定压缩范围或修改输入条目。
// 调用方负责请求序号、用量记录、取消和错误处理；返回值只能作为不可信上下文内容。
type RequestSummaryFunc func(context.Context, appcontext.ModelContext, int) (string, error)
