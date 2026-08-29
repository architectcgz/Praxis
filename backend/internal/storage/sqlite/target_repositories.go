package sqlite

// Target orchestration storage deliberately uses distinct tables while the
// pre-release converter remains to be implemented. Production composition can
// select these repositories without accidentally reading legacy aggregates.
type TargetRepositories struct {
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
