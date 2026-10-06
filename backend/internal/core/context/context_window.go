package context

import "fmt"

// ContextWindowExceededError 表示一次请求连同输出预留会超出模型的上下文窗口。
// provider adapter 在真正发送前返回它，task engine 据此产出稳定的资源上限失败。
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
//
// ponytail: 字节数 / 4 仍是近似，非 ASCII（中文 3 字节/字）会低估。它只用于提前拒绝明显
// 超窗的请求，provider 返回的 400 才是权威；接入真实 tokenizer 后替换此处。
func ExceedsContextWindow(payload []byte, maxOutputTokens, contextWindow int) error {
	if contextWindow <= 0 {
		return nil
	}
	estimated := len(payload) / 4
	if estimated+maxOutputTokens > contextWindow {
		return &ContextWindowExceededError{
			EstimatedInputTokens: estimated,
			MaxOutputTokens:      maxOutputTokens,
			ContextWindow:        contextWindow,
		}
	}
	return nil
}
