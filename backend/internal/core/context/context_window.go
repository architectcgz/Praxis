package context

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// EstimateTokens 估算文本 token 数，ASCII 按四字节一个 token，非 ASCII 按 UTF-8 字节保守计数。
// ponytail: 未接入各模型 tokenizer；该估算用于压缩预警和本地保护，Provider 才是窗口校验的权威。
func EstimateTokens(text string) int {
	ascii, other := 0, 0
	for _, value := range text {
		if value < utf8.RuneSelf {
			ascii++
		} else {
			other += utf8.RuneLen(value)
		}
	}
	return (ascii+3)/4 + other
}

// EstimatedTokens 估算上下文正文和结构开销，工具定义需由请求边界另行预留。
func (c ModelContext) EstimatedTokens() (int, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	return EstimateTokens(string(payload)), nil
}

// ContextWindowExceededError 表示一次请求连同输出预留会超出模型的上下文窗口。
// 构建、压缩或 Provider 在无法容纳必要内容时返回它，loop 据此分类为资源上限失败。
type ContextWindowExceededError struct {
	EstimatedInputTokens int
	MaxOutputTokens      int
	ContextWindow        int
}

func (e *ContextWindowExceededError) Error() string {
	return fmt.Sprintf(
		"estimated %d input tokens plus %d reserved output tokens exceed the model context window %d",
		e.EstimatedInputTokens, e.MaxOutputTokens, e.ContextWindow,
	)
}

// ExceedsContextWindow 报告一次已编码请求是否装不进模型的上下文窗口：把请求体估算成
// 输入 token，加上本次预留的输出 token，再与窗口比较。装得下、或窗口未记录（<=0）时
// 返回 nil。
//
// 判定放在 provider adapter 而不是 task engine：只有 adapter 知道本协议最终序列
// 化出的请求体（含工具 schema、system prompt、tool result），引擎侧的全局启发式必然漏项。
func ExceedsContextWindow(payload []byte, maxOutputTokens, contextWindow int) error {
	if contextWindow <= 0 {
		return nil
	}
	estimated := EstimateTokens(string(payload))
	if estimated+maxOutputTokens > contextWindow {
		return &ContextWindowExceededError{
			EstimatedInputTokens: estimated,
			MaxOutputTokens:      maxOutputTokens,
			ContextWindow:        contextWindow,
		}
	}
	return nil
}
