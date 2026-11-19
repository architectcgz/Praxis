package entry

import (
	modelusage "praxis/internal/core/model/usage"
	"time"
)

// RunFailureCode 是 Run 终态的稳定原因。
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

type RunOutcome string

const (
	OutcomeCompleted   RunOutcome = "completed"
	OutcomeYielded     RunOutcome = "yielded"
	OutcomeFailed      RunOutcome = "failed"
	OutcomeInterrupted RunOutcome = "interrupted"
)

// RunStats 是各 Run 终态都需要保存的时间、用量和输出信息。
type RunStats struct {
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

// RunTerminal 提供已结束执行的载荷及其查询所需的字段。
type RunTerminal interface {
	Payload
	Outcome() RunOutcome
	Stats() RunStats
	Failure() (RunFailureCode, string)
}

func (s RunStats) Stats() RunStats { return s }

type RunFailure struct {
	Code    RunFailureCode `json:"failureCode,omitempty"`
	Message string         `json:"failureMessage,omitempty"`
}

func (f RunFailure) Failure() (RunFailureCode, string) { return f.Code, f.Message }

type RunCompleted struct{ RunStats }

func (RunCompleted) isPayload()                        {}
func (RunCompleted) Outcome() RunOutcome               { return OutcomeCompleted }
func (RunCompleted) Failure() (RunFailureCode, string) { return "", "" }

type RunFailed struct {
	RunStats
	RunFailure
}

func (RunFailed) isPayload()          {}
func (RunFailed) Outcome() RunOutcome { return OutcomeFailed }

type RunInterrupted struct {
	RunStats
	RunFailure
}

func (RunInterrupted) isPayload()          {}
func (RunInterrupted) Outcome() RunOutcome { return OutcomeInterrupted }

type SubagentYielded struct{ RunStats }

func (SubagentYielded) isPayload()                        {}
func (SubagentYielded) Outcome() RunOutcome               { return OutcomeYielded }
func (SubagentYielded) Failure() (RunFailureCode, string) { return "", "" }
