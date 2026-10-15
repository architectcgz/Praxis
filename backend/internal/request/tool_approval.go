package request

import (
	"praxis/internal/contracts"
	"strings"
)

type ToolDecision struct {
	SessionID       contracts.SessionID
	AgentID         contracts.AgentID
	TaskID          contracts.TaskID
	CallID          string
	ArgumentsDigest string
}

func NewToolDecision(value ToolDecision) (ToolDecision, error) {
	value.CallID = strings.TrimSpace(value.CallID)
	value.ArgumentsDigest = strings.TrimSpace(value.ArgumentsDigest)
	if value.SessionID == "" || value.AgentID == "" || value.TaskID == "" || value.CallID == "" || value.ArgumentsDigest == "" {
		return ToolDecision{}, contracts.New(contracts.InvalidRequest, "tool approval identity is required")
	}
	return value, nil
}
