package request

import (
	"praxis/internal/contracts"
	"strings"
)

type ToolDecision struct {
	SessionID contracts.SessionID
	AgentID   contracts.AgentID
	TaskID    contracts.TaskID
	CallID    string
}

func NewToolDecision(value ToolDecision) (ToolDecision, error) {
	value.CallID = strings.TrimSpace(value.CallID)
	if value.SessionID == "" || value.AgentID == "" || value.TaskID == "" || value.CallID == "" {
		return ToolDecision{}, contracts.New(contracts.InvalidRequest, "tool approval identity is required")
	}
	return value, nil
}
