package entry

import (
	"encoding/json"
	"fmt"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
	"time"
)

// Payload 是执行事实的封闭集合，与进程通知分别定义。
type Payload interface{ factKind() string }

type UserInput struct {
	MessageID string
	Text      string
}

func (UserInput) factKind() string { return "user_input" }

// RunPrepared 只记录创建时的归属信息，不保存可变执行状态或冻结配置。
type RunPrepared struct {
	AgentName    string
	DefinitionID identity.AgentDefinitionID
	SourceTaskID identity.TaskID
	InputEntryID string
}

func (RunPrepared) factKind() string { return "run_prepared" }

type RunStarted struct{ At time.Time }

func (RunStarted) factKind() string { return "run_started" }

type RunEndingStarted struct{}

func (RunEndingStarted) factKind() string { return "run_ending" }

type RunCompleted struct{ Result RunResult }

func (RunCompleted) factKind() string { return "run_completed" }

type RunFailed struct{ Result RunResult }

func (RunFailed) factKind() string { return "run_failed" }

type RunInterrupted struct{ Result RunResult }

func (RunInterrupted) factKind() string { return "run_interrupted" }

type SubagentYielded struct{ Result RunResult }

func (SubagentYielded) factKind() string { return "subagent_yielded" }

type TurnStarted struct{ Turn TurnRecord }

func (TurnStarted) factKind() string { return "turn_started" }

type TurnCompleted struct{ Turn TurnRecord }

func (TurnCompleted) factKind() string { return "turn_completed" }

type TurnFailed struct{ Turn TurnRecord }

func (TurnFailed) factKind() string { return "turn_failed" }

type TurnInterrupted struct{ Turn TurnRecord }

func (TurnInterrupted) factKind() string { return "turn_interrupted" }

type ModelRequestStarted struct {
	Request ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestStarted) factKind() string { return "model_request_started" }

type ModelRequestCompleted struct {
	Request ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestCompleted) factKind() string { return "model_request_completed" }

type ModelRequestFailed struct {
	Request ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestFailed) factKind() string { return "model_request_failed" }

type ModelRequestInterrupted struct {
	Request ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestInterrupted) factKind() string { return "model_request_interrupted" }

type storedPayload struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

func encodePayload(value Payload) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(storedPayload{Kind: value.factKind(), Data: data})
}

func decodePayload(data json.RawMessage) (Payload, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var stored storedPayload
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	switch stored.Kind {
	case "subagent_instruction_accepted":
		var value SubagentInstructionAccepted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "subagent_instruction_received":
		var value SubagentInstructionReceived
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "subagent_report_accepted":
		var value SubagentReportAccepted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "subagent_report_received":
		var value SubagentReportReceived
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "user_input":
		var value UserInput
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "run_prepared":
		var value RunPrepared
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "run_started":
		var value RunStarted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "run_ending":
		return RunEndingStarted{}, nil
	case "run_completed":
		var value RunCompleted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "run_failed":
		var value RunFailed
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "run_interrupted":
		var value RunInterrupted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "subagent_yielded":
		var value SubagentYielded
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "turn_started":
		var value TurnStarted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "turn_completed":
		var value TurnCompleted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "turn_failed":
		var value TurnFailed
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "turn_interrupted":
		var value TurnInterrupted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "model_request_started":
		var value ModelRequestStarted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "model_request_completed":
		var value ModelRequestCompleted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "model_request_failed":
		var value ModelRequestFailed
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	case "model_request_interrupted":
		var value ModelRequestInterrupted
		err := json.Unmarshal(stored.Data, &value)
		return value, err
	default:
		return nil, fmt.Errorf("unknown entry payload %q", stored.Kind)
	}
}

func (e Entry) RequestFact() (*ModelRequestRecord, *timing.Record) {
	switch p := e.Payload.(type) {
	case ModelRequestStarted:
		return &p.Request, p.Timing
	case ModelRequestCompleted:
		return &p.Request, p.Timing
	case ModelRequestFailed:
		return &p.Request, p.Timing
	case ModelRequestInterrupted:
		return &p.Request, p.Timing
	default:
		return nil, nil
	}
}

func (e Entry) ExecutionResult() *RunResult {
	switch v := e.Payload.(type) {
	case RunCompleted:
		return &v.Result
	case RunFailed:
		return &v.Result
	case RunInterrupted:
		return &v.Result
	case SubagentYielded:
		return &v.Result
	default:
		return nil
	}
}

func ModelRequestEntry(request ModelRequestRecord, measured *timing.Record) Entry {
	var payload Payload
	suffix := "finished"
	switch request.Status {
	case RequestRunning:
		payload = ModelRequestStarted{Request: request, Timing: measured}
		suffix = "started"
	case RequestCompleted:
		payload = ModelRequestCompleted{Request: request, Timing: measured}
	case RequestFailed:
		payload = ModelRequestFailed{Request: request, Timing: measured}
	case RequestInterrupted:
		payload = ModelRequestInterrupted{Request: request, Timing: measured}
	}
	return Entry{
		ID:      "request:" + request.ID.String() + ":" + suffix,
		RunID:   request.RunID.String(),
		TurnID:  request.TurnID.String(),
		Payload: payload,
	}
}

func (e Entry) InputMessageID() string {
	if p, ok := e.Payload.(UserInput); ok {
		return p.MessageID
	}
	return ""
}
func (e Entry) IsMetadata() bool {
	if e.Payload == nil {
		return false
	}
	switch e.Payload.(type) {
	case UserInput, SubagentReportAccepted, SubagentInstructionAccepted:
		return false
	default:
		return true
	}
}

type SubagentReportReceived struct{ Report SubagentResult }

func (SubagentReportReceived) factKind() string { return "subagent_report_received" }

type SubagentReportAccepted struct{ ReportID string }

func (SubagentReportAccepted) factKind() string { return "subagent_report_accepted" }

type SubagentInstructionReceived struct {
	MessageID string
	Text      string
}

func (SubagentInstructionReceived) factKind() string { return "subagent_instruction_received" }

type SubagentInstructionAccepted struct{ MessageID string }

func (SubagentInstructionAccepted) factKind() string { return "subagent_instruction_accepted" }

type SubagentResult struct {
	ReportID     string
	AgentID      identity.AgentID
	RunID        identity.RunID
	ParentRunID  identity.RunID
	ParentCallID string
	Outcome      RunOutcome
	FailureCode  RunFailureCode
	Text         string
	Final        bool
	Partial      bool
}
type SubagentReceipt struct {
	ReportID string           `json:"reportId"`
	AgentID  identity.AgentID `json:"agentId"`
	Outcome  RunOutcome       `json:"outcome"`
	Final    bool             `json:"final"`
}
