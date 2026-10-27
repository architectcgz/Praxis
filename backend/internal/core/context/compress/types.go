// Package compress 提供单摘要历史压缩和当前工具交互的精简策略。
package compress

import (
	"context"
	appcontext "praxis/internal/core/context"
)

// SummaryFormat 由应用提供提示词和历史资料文本格式。
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

// RequestSummaryFunc 只负责无工具的摘要模型请求，不决定压缩范围或修改历史。
// 调用方负责请求序号、用量记录、取消和错误处理；返回值只能作为不可信历史数据。
type RequestSummaryFunc func(context.Context, appcontext.ModelContext, int) (string, error)
