package agent

// FinalOutput 记录已提交的最终 Assistant 输出证据。
type FinalOutput struct {
	AssistantID string
	StopReason  string
}
