package entry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/identity"
	"time"
)

// Payload 是执行事实的封闭集合，与进程通知分别定义。
type Payload interface{ kind() string }

// RunPrepared 只记录创建时的归属信息，不保存可变执行状态或冻结配置。
type RunPrepared struct {
	AgentName    string
	DefinitionID identity.AgentDefinitionID
	SourceTaskID identity.TaskID
}

func (RunPrepared) kind() string { return kindRunPrepared }

type RunStarted struct{ At time.Time }

func (RunStarted) kind() string { return kindRunStarted }

type RunEndingStarted struct{}

func (RunEndingStarted) kind() string { return kindRunEnding }

// RunFailureCode 是 Run 终态的稳定原因。
type RunFailureCode string

const (
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

type RunCompleted struct{ RunStats }

func (RunCompleted) kind() string { return kindRunCompleted }

type RunFailed struct {
	RunStats
	Code    RunFailureCode `json:"failureCode"`
	Message string         `json:"failureMessage,omitempty"`
}

func (RunFailed) kind() string { return kindRunFailed }

type RunInterrupted struct {
	RunStats
	Reason string `json:"reason"`
}

func (RunInterrupted) kind() string { return kindRunInterrupted }

type SubagentYielded struct{ RunStats }

func (SubagentYielded) kind() string { return kindSubagentYielded }

type TurnStarted struct{}

func (TurnStarted) kind() string { return kindTurnStarted }

type TurnCompleted struct{}

func (TurnCompleted) kind() string { return kindTurnCompleted }

type TurnFailed struct{}

func (TurnFailed) kind() string { return kindTurnFailed }

type TurnInterrupted struct{}

func (TurnInterrupted) kind() string { return kindTurnInterrupted }

const (
	kindMessage         = "message"
	kindSummary         = "summary"
	kindRunPrepared     = "run_prepared"
	kindRunStarted      = "run_started"
	kindRunEnding       = "run_ending"
	kindRunCompleted    = "run_completed"
	kindRunFailed       = "run_failed"
	kindRunInterrupted  = "run_interrupted"
	kindSubagentYielded = "subagent_yielded"
	kindTurnStarted     = "turn_started"
	kindTurnCompleted   = "turn_completed"
	kindTurnFailed      = "turn_failed"
	kindTurnInterrupted = "turn_interrupted"
	kindModelRequest    = "model_request"
)

// decodePayload 使用 Entry 顶层的 kind 恢复具体载荷。
func decodePayload(kind string, data json.RawMessage) (Payload, error) {
	if kind == "" {
		return nil, errors.New("history entry requires a top-level kind")
	}
	if len(data) == 0 || string(data) == "null" {
		return nil, fmt.Errorf("entry kind %q requires a payload", kind)
	}
	switch kind {
	case kindMessage:
		return decodePayloadData[Message](data)
	case kindSummary:
		return decodeSummary(data)
	case kindRunPrepared:
		return decodePayloadData[RunPrepared](data)
	case kindRunStarted:
		return decodePayloadData[RunStarted](data)
	case kindRunEnding:
		return decodePayloadData[RunEndingStarted](data)
	case kindRunCompleted:
		return decodePayloadData[RunCompleted](data)
	case kindRunFailed:
		return decodePayloadData[RunFailed](data)
	case kindRunInterrupted:
		return decodePayloadData[RunInterrupted](data)
	case kindSubagentYielded:
		return decodePayloadData[SubagentYielded](data)
	case kindTurnStarted:
		return decodePayloadData[TurnStarted](data)
	case kindTurnCompleted:
		return decodePayloadData[TurnCompleted](data)
	case kindTurnFailed:
		return decodePayloadData[TurnFailed](data)
	case kindTurnInterrupted:
		return decodePayloadData[TurnInterrupted](data)
	case kindModelRequest:
		return decodePayloadData[ModelRequest](data)
	default:
		return nil, fmt.Errorf("unknown entry kind %q", kind)
	}
}

func decodePayloadData[T Payload](data json.RawMessage) (Payload, error) {
	var value T
	if err := decodeFields(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeFields(data json.RawMessage, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}
