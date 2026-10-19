package toolexecution

// FinalOutput 保存最终 Assistant 的提交证据。
type FinalOutput struct {
	AssistantID string
	StopReason  string
}
