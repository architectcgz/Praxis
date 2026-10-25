package toolexecution

import (
	"praxis/internal/clock"
	"praxis/internal/repository"
)

type Dependencies struct {
	Rules    ApprovalRules
	Tasks    repository.AgentExecution
	Entries  repository.Entries
	Calls    repository.ToolCallQueries
	Sessions repository.Session
	Clock    clock.Clock
}

// Service 准备审核输入、保存正常回执，并为恢复编排构造缺失的历史条目。
type Service struct {
	deps  Dependencies
	clock clock.Clock
}

func NewService(deps Dependencies) *Service {
	return &Service{
		deps:  deps,
		clock: deps.Clock,
	}
}
