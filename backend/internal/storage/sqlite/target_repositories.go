package sqlite

// TargetRepositories groups the repositories used by the orchestration model.
type TargetRepositories struct {
	Projects   ProjectRepository
	Workspaces WorkspaceRepository
	Sessions   SessionRepository
	Groups     AgentGroupRepository
	Agents     AgentRepository
	Executions AgentExecutionRepository
	QueuedWork QueuedWorkRepository
	Waits      WaitConditionRepository
	Controls   AgentControlRequestRepository
	Deliveries ContextDeliveryRepository
}

func (s *Store) TargetRepositories() TargetRepositories {
	return TargetRepositories{
		Projects:   ProjectRepository{s},
		Workspaces: WorkspaceRepository{s},
		Sessions:   SessionRepository{s},
		Groups:     AgentGroupRepository{s},
		Agents:     AgentRepository{s},
		Executions: AgentExecutionRepository{s},
		QueuedWork: QueuedWorkRepository{s},
		Waits:      WaitConditionRepository{s},
		Controls:   AgentControlRequestRepository{s},
		Deliveries: ContextDeliveryRepository{s},
	}
}
