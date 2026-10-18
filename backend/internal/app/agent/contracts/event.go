package contracts

import (
	"encoding/json"

	timingcontracts "praxis/internal/app/timing/contracts"
	usagecontracts "praxis/internal/app/usage/contracts"
)

// AgentEvent 是瞬时通知；终态后仍应查询持久化事实。
type AgentEvent struct {
	Kind           string
	SessionID      string
	AgentID        string
	TaskID         string
	Outcome        string
	FailureCode    string
	FailureMessage string
	TurnID         string
	Text           string
	CallID         string
	Name           string
	Input          json.RawMessage
	Result         string
	IsError        bool
	Error          string
	Timing         *timingcontracts.OperationTiming
	Usage          *usagecontracts.ModelUsage
}
