package toolexecution

import (
	"praxis/internal/clock"
	"praxis/internal/repository"
	approvalservice "praxis/internal/service/approval"
)

type Dependencies struct {
	ApprovalConfig approvalservice.ConfigSource
	Tasks          repository.AgentExecution
	Entries        repository.Entries
	Calls          repository.ToolCallQueries
	Sessions       repository.Session
	Projects       repository.Project
	Clock          clock.Clock
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
