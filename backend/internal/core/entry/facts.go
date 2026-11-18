package entry

import (
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/core/entry/record"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
)

func (e Entry) RequestFact() (*ModelRequestRecord, *timing.Record) {
	switch p := e.Payload.(type) {
	case entryPayload.ModelRequestStarted:
		return &p.Request, p.Timing
	case entryPayload.ModelRequestCompleted:
		return &p.Request, p.Timing
	case entryPayload.ModelRequestFailed:
		return &p.Request, p.Timing
	case entryPayload.ModelRequestInterrupted:
		return &p.Request, p.Timing
	default:
		return nil, nil
	}
}

func (e Entry) ExecutionResult() *RunResult {
	switch v := e.Payload.(type) {
	case entryPayload.RunCompleted:
		return &v.Result
	case entryPayload.RunFailed:
		return &v.Result
	case entryPayload.RunInterrupted:
		return &v.Result
	case entryPayload.SubagentYielded:
		return &v.Result
	default:
		return nil
	}
}

func ModelRequestEntry(request ModelRequestRecord, measured *timing.Record) Entry {
	var payload entryPayload.Payload
	suffix := "finished"
	switch request.Status {
	case RequestRunning:
		payload = entryPayload.ModelRequestStarted{Request: request, Timing: measured}
		suffix = "started"
	case RequestCompleted:
		payload = entryPayload.ModelRequestCompleted{Request: request, Timing: measured}
	case RequestFailed:
		payload = entryPayload.ModelRequestFailed{Request: request, Timing: measured}
	case RequestInterrupted:
		payload = entryPayload.ModelRequestInterrupted{Request: request, Timing: measured}
	}
	return Entry{
		ID:      "request:" + request.ID.String() + ":" + suffix,
		RunID:   request.RunID.String(),
		TurnID:  request.TurnID.String(),
		Payload: payload,
	}
}

func (e Entry) InputMessageID() string {
	if p, ok := e.Payload.(entryPayload.UserInput); ok {
		return p.MessageID
	}
	return ""
}

func (e Entry) IsMetadata() bool {
	if e.Payload == nil {
		return false
	}
	switch e.Payload.(type) {
	case entryPayload.UserInput, entryPayload.SubagentReportAccepted, entryPayload.SubagentInstructionAccepted:
		return false
	default:
		return true
	}
}

type SubagentResult = record.SubagentResult

type SubagentReceipt struct {
	ReportID string           `json:"reportId"`
	AgentID  identity.AgentID `json:"agentId"`
	Outcome  RunOutcome       `json:"outcome"`
	Final    bool             `json:"final"`
}
