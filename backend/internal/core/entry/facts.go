package entry

import (
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/identity"
)

type SubagentReceipt struct {
	ReportID string                  `json:"reportId"`
	AgentID  identity.AgentID        `json:"agentId"`
	Outcome  entryPayload.RunOutcome `json:"outcome"`
	Final    bool                    `json:"final"`
}
