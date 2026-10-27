package toolexecution

import (
	"praxis/internal/repository"
	approvalservice "praxis/internal/service/approval"
)

type Dependencies struct {
	ApprovalConfig approvalservice.ConfigSource
	Calls          repository.ToolCallQueries
	Sessions       repository.Session
	Projects       repository.Project
}

// Service 判断审批规则，并为恢复编排构造缺失的历史条目。
type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{
		deps: deps,
	}
}
