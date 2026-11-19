package payload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/identity"
	"time"
)

// Payload 是执行事实的封闭集合，与进程通知分别定义。
type Payload interface{ isPayload() }

// RunPrepared 只记录创建时的归属信息，不保存可变执行状态或冻结配置。
type RunPrepared struct {
	AgentName    string
	DefinitionID identity.AgentDefinitionID
	SourceTaskID identity.TaskID
	InputEntryID string
}

func (RunPrepared) isPayload() {}

type RunStarted struct{ At time.Time }

func (RunStarted) isPayload() {}

type RunEndingStarted struct{}

func (RunEndingStarted) isPayload() {}

type TurnStarted struct{}

func (TurnStarted) isPayload() {}

type TurnCompleted struct{}

func (TurnCompleted) isPayload() {}

type TurnFailed struct{}

func (TurnFailed) isPayload() {}

type TurnInterrupted struct{}

func (TurnInterrupted) isPayload() {}

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

// Encode 将历史条目的类型与载荷分开返回；内存中不保存额外的 kind。
func Encode(value Payload) (string, json.RawMessage, error) {
	var kind string
	switch value.(type) {
	case Message:
		kind = kindMessage
	case Summary:
		kind = kindSummary
	case RunPrepared:
		kind = kindRunPrepared
	case RunStarted:
		kind = kindRunStarted
	case RunEndingStarted:
		kind = kindRunEnding
	case RunCompleted:
		kind = kindRunCompleted
	case RunFailed:
		kind = kindRunFailed
	case RunInterrupted:
		kind = kindRunInterrupted
	case SubagentYielded:
		kind = kindSubagentYielded
	case TurnStarted:
		kind = kindTurnStarted
	case TurnCompleted:
		kind = kindTurnCompleted
	case TurnFailed:
		kind = kindTurnFailed
	case TurnInterrupted:
		kind = kindTurnInterrupted
	case ModelRequest:
		kind = kindModelRequest
	default:
		return "", nil, fmt.Errorf("unsupported entry payload type %T", value)
	}
	data, err := json.Marshal(value)
	return kind, data, err
}

// Decode 使用 Entry 顶层的 kind 恢复具体载荷。
func Decode(kind string, data json.RawMessage) (Payload, error) {
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
