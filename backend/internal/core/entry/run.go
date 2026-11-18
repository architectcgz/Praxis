package entry

import (
	agentmodel "praxis/internal/core/agent"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/dto"
	"praxis/internal/identity"
	"time"
)

// TurnRecord 表达一轮 ReAct 迭代，请求尝试及审核不单独创建轮次。
type TurnRecord struct {
	ID        identity.TurnID
	RunID     identity.RunID
	StartedAt time.Time
	EndedAt   time.Time
}

// RunFailureCode 是任务持久化结果的稳定失败原因。
type RunFailureCode string

const (
	RunFailureTimeout             RunFailureCode = "execution_timeout"
	RunFailureProviderUnavailable RunFailureCode = "provider_unavailable"
	RunFailureProvider            RunFailureCode = "execution_provider_error"
	RunFailureTool                RunFailureCode = "execution_tool_error"
	RunFailurePolicyBlocked       RunFailureCode = "execution_policy_blocked"
	RunFailureApprovalRequired    RunFailureCode = "execution_approval_required"
	RunFailureResourceLimit       RunFailureCode = "execution_resource_limit"
	RunFailureStorage             RunFailureCode = "execution_storage_error"
	RunFailureContract            RunFailureCode = "execution_contract_error"
	RunFailureBusy                RunFailureCode = "execution_busy"
	RunFailureInterrupted         RunFailureCode = "execution_interrupted"
	RunFailureRequestCanceled     RunFailureCode = "request_canceled"
	RunFailureCancelled           RunFailureCode = "execution_cancelled"
	RunFailureInternal            RunFailureCode = "execution_failed"
	RunFailureInvalidOutcome      RunFailureCode = "execution_invalid_outcome"
)

// MaxRunFailureMessageRunes 限制持久化失败详情的字符数。
const MaxRunFailureMessageRunes = 4096

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

func (e Entry) IsRun() bool { return e.ExecutionResult() != nil }

type RunStatus string

const (
	RunStarting RunStatus = "starting"
	RunRunning  RunStatus = "running"
	RunEnding   RunStatus = "ending"
	RunEnded    RunStatus = "ended"
)

type RunOutcome string

const (
	OutcomeCompleted   RunOutcome = "completed"
	OutcomeYielded     RunOutcome = "yielded"
	OutcomeFailed      RunOutcome = "failed"
	OutcomeInterrupted RunOutcome = "interrupted"
)

// AgentState 从结束结果提取展示状态，不把失败原因作为运行阶段。
func (r RunResult) AgentState() agentmodel.AgentState {
	switch RunOutcome(r.Outcome) {
	case OutcomeInterrupted:
		return agentmodel.AgentInterrupted
	case OutcomeFailed:
		return agentmodel.AgentFailed
	default:
		return agentmodel.AgentIdle
	}
}

// Snapshot 直接构造查询与通知共用的 DTO，不重建运行时对象。
func (r RunResult) Snapshot(id identity.RunID) dto.RunSummary {
	return dto.RunSummary{
		ID:          id.String(),
		Status:      string(RunEnded),
		StartedAt:   r.StartedAt,
		Iterations:  r.Iterations,
		ToolCalls:   r.ToolCalls,
		OutputBytes: r.OutputBytes,
		Result: &dto.RunResult{
			Outcome:        r.Outcome,
			EndedAt:        r.EndedAt,
			FailureCode:    r.FailureCode,
			FailureMessage: r.FailureMessage,
		},
	}
}
