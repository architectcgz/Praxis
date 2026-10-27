package toolexecution

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"praxis/internal/repository"
	approvalservice "praxis/internal/service/approval"
)

type Dependencies struct {
	ApprovalConfig approvalservice.ConfigSource
	AppendEntry    func(context.Context, identity.SessionID, sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error)
	Calls          repository.ToolCallQueries
	Sessions       repository.Session
	Projects       repository.Project
}

// Service 判断审批规则、保存正常回执，并为恢复编排构造缺失的历史条目。
type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{
		deps: deps,
	}
}
