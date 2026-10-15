package context

// ContextBuilder 校验、复制并规范化通用内容，拒绝连必要原文都无法容纳的窗口。
// 可压缩历史允许暂时超窗，由请求边界在调用模型前执行压缩。
type ContextBuilder struct{}

func NewContextBuilder() ContextBuilder { return ContextBuilder{} }

func (ContextBuilder) Build(content Content, limits Limits) (ModelContext, error) {
	if err := limits.Validate(); err != nil {
		return ModelContext{}, err
	}
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
	if err := value.Validate(); err != nil {
		return ModelContext{}, err
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
