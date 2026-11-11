package entry

import (
	modelusage "praxis/internal/core/model/usage"
	"time"
)

// RunResult 是完成收尾后发布的不可变执行结果，不包含冻结配置或模型上下文。
type RunResult struct {
	Outcome          string            `json:"outcome"`
	FailureCode      string            `json:"failureCode,omitempty"`
	FailureMessage   string            `json:"failureMessage,omitempty"`
	StartedAt        time.Time         `json:"startedAt"`
	EndedAt          time.Time         `json:"endedAt"`
	Usage            modelusage.Totals `json:"usage"`
	ModelRequests    uint64            `json:"modelRequests"`
	Iterations       uint64            `json:"iterations"`
	OutputBytes      int64             `json:"outputBytes"`
	ToolCalls        int               `json:"toolCalls"`
	FinalAssistantID string            `json:"finalAssistantId,omitempty"`
	FinalStopReason  string            `json:"finalStopReason,omitempty"`
}

func (e ContextEntry) IsRun() bool { return e.RunResult != nil }
