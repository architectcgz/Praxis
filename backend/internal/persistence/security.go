package persistence

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
)

type AgentSecurityPolicyRepository interface {
	GetCurrent(context.Context, domainfoundation.AgentID) (domainsecurity.AgentSecurityPolicy, error)
	Save(context.Context, domainfoundation.AgentID, domainsecurity.AgentSecurityPolicy) error
}
