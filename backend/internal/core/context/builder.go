package context

// ContextBuilder 复制通用内容、丢弃展示用思考块并保持顺序，拒绝连必要原文都无法容纳的窗口。
// 输入内容的结构约束由调用方的输入边界保证，本包只做复制与容量检查。
// 可压缩历史允许暂时超窗，由请求边界在调用模型前执行压缩。
type ContextBuilder struct{}

func NewContextBuilder() ContextBuilder { return ContextBuilder{} }

// Build 返回去掉思考内容的模型上下文，并按 Protected 原文估算必要容量。
// 全部内容均为思考块的条目会被跳过；服务入口已保证当前输入存在模型可见内容，
// 因此调用方不会拿到空条目上下文，本方法不再单独检查。
func (ContextBuilder) Build(content Content, limits Limits) (ModelContext, error) {
	value := ModelContext{SystemPrompt: content.SystemPrompt}
	minimum := ModelContext{SystemPrompt: content.SystemPrompt}
	for _, entry := range content.Entries {
		copy := entry
		copy.Content = nil
		for _, block := range entry.Content {
			if block.Kind != ContextBlockThinking {
				copy.Content = append(copy.Content, block.Clone())
			}
		}
		if len(copy.Content) == 0 {
			continue
		}
		value.Entries = append(value.Entries, copy)
		if copy.Protected {
			minimum.Entries = append(minimum.Entries, copy)
		}
	}
	tokens, err := minimum.EstimatedTokens()
	if err != nil {
		return ModelContext{}, err
	}
	if tokens > limits.ContextWindow-limits.MaxOutputTokens {
		return ModelContext{}, &ContextWindowExceededError{EstimatedInputTokens: tokens, MaxOutputTokens: limits.MaxOutputTokens, ContextWindow: limits.ContextWindow}
	}
	return value, nil
}
