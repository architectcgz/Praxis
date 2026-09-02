package persistence

import (
	"context"

	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
)

type AgentSecurityPolicyRepository interface {
	GetCurrent(context.Context, domainfoundation.AgentID) (domainsecurity.AgentSecurityPolicy, error)
	Save(context.Context, domainfoundation.AgentID, domainsecurity.AgentSecurityPolicy) error
}
