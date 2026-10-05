package agent

import (
	"errors"
	"strings"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
)

type AgentMessage struct {
	AgentID contracts.AgentID
	Data    sessionmodel.MessageData
}

// Validate 校验消息归属到 Agent 且消息数据有效。
func (m AgentMessage) Validate() error {
	if strings.TrimSpace(m.AgentID.String()) == "" {
		return errors.New("agent message owner is required")
	}
	return m.Data.Validate()
}
