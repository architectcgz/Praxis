package repository

import (
	"praxis/internal/contracts"
	securitymodel "praxis/internal/core/security"

	"context"
)

type AgentSecurityPolicyRepository interface {
	GetCurrent(context.Context, contracts.AgentID) (securitymodel.AgentSecurityPolicy, error)
	GetByRevision(context.Context, contracts.AgentID, uint64) (securitymodel.AgentSecurityPolicy, bool, error)
	Save(context.Context, contracts.AgentID, securitymodel.AgentSecurityPolicy) error
}
