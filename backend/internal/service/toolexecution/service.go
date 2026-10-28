package toolexecution

import (
	"praxis/internal/repository"
	approvalservice "praxis/internal/service/approval"
)

type Dependencies struct {
	ApprovalConfig approvalservice.ConfigSource
	Sessions       repository.Session
}

// Service 读取工具归属和审批规则，准备审核输入及预算。
type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{
		deps: deps,
	}
}
