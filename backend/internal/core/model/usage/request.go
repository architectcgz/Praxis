package usage

// Usage 记录一次模型请求的已知用量；输入计数包含已知的缓存读取和写入。
type Usage struct {
	InputTokens              *int64 `json:"inputTokens,omitempty"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

func (u Usage) Reported() bool {
	return u.InputTokens != nil || u.OutputTokens != nil || u.CacheReadInputTokens != nil || u.CacheCreationInputTokens != nil
}
