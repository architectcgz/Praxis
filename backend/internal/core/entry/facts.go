package entry

import (
	"praxis/internal/identity"
)

type SubagentReceipt struct {
	ReportID string           `json:"reportId"`
	AgentID  identity.AgentID `json:"agentId"`
	Outcome  RunOutcome       `json:"outcome"`
	Final    bool             `json:"final"`
}
