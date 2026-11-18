package payload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"praxis/internal/core/entry/record"
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

type TurnStarted struct{}

func (TurnStarted) factKind() string { return "turn_started" }

type TurnCompleted struct{}

func (TurnCompleted) factKind() string { return "turn_completed" }

type TurnFailed struct{}

func (TurnFailed) factKind() string { return "turn_failed" }

type TurnInterrupted struct{}

func (TurnInterrupted) factKind() string { return "turn_interrupted" }

type ModelRequestStarted struct {
	Request record.ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestStarted) factKind() string { return "model_request_started" }

type ModelRequestCompleted struct {
	Request record.ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestCompleted) factKind() string { return "model_request_completed" }

type ModelRequestFailed struct {
	Request record.ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestFailed) factKind() string { return "model_request_failed" }

type ModelRequestInterrupted struct {
	Request record.ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestInterrupted) factKind() string { return "model_request_interrupted" }

type SubagentReport struct {
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

type SubagentReportReceived struct{ Report SubagentReport }

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

type storedPayload struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// Encode 将具体载荷写成历史使用的 kind 和 data 字段。
func Encode(value Payload) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(storedPayload{Kind: value.factKind(), Data: data})
}

// Decode 从历史的 kind 恢复具体载荷。
func Decode(data json.RawMessage) (Payload, error) {
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
		return decodePayloadData[RunCompleted](stored.Data)
	case "run_failed":
		return decodePayloadData[RunFailed](stored.Data)
	case "run_interrupted":
		return decodePayloadData[RunInterrupted](stored.Data)
	case "subagent_yielded":
		return decodePayloadData[SubagentYielded](stored.Data)
	case "turn_started":
		return decodePayloadData[TurnStarted](stored.Data)
	case "turn_completed":
		return decodePayloadData[TurnCompleted](stored.Data)
	case "turn_failed":
		return decodePayloadData[TurnFailed](stored.Data)
	case "turn_interrupted":
		return decodePayloadData[TurnInterrupted](stored.Data)
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

func decodePayloadData[T Payload](data json.RawMessage) (Payload, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}
