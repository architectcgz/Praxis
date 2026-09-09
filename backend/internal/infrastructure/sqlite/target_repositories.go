package sqlite

import "praxis/internal/persistence"

// TargetRepositories groups the repositories used by the orchestration model.
type TargetRepositories struct {
	Commands          CommandReceiptRepository
	Projects          ProjectRepository
	Workspaces        WorkspaceRepository
	Sessions          SessionRepository
	Contexts          SessionContextRepository
	Policies          AgentSecurityPolicyRepository
	Agents            AgentRepository
	Executions        AgentExecutionRepository
	SecuritySnapshots ExecutionSecuritySnapshotRepository
	ToolInvocations   ToolInvocationRepository
	QueuedWork        QueuedWorkRepository
	Waits             WaitConditionRepository
	Controls          AgentControlRequestRepository
	Deliveries        ContextDeliveryRepository
	Events            persistence.EventRepository
}

func (s *Store) TargetRepositories() TargetRepositories {
	return TargetRepositories{
		Commands:          CommandReceiptRepository{s},
		Projects:          ProjectRepository{s},
		Workspaces:        WorkspaceRepository{s},
		Sessions:          SessionRepository{s},
		Contexts:          SessionContextRepository{s},
		Policies:          AgentSecurityPolicyRepository{s},
		Agents:            AgentRepository{s},
		Executions:        AgentExecutionRepository{s},
		SecuritySnapshots: ExecutionSecuritySnapshotRepository{s},
		ToolInvocations:   ToolInvocationRepository{s},
		QueuedWork:        QueuedWorkRepository{s},
		Waits:             WaitConditionRepository{s},
		Controls:          AgentControlRequestRepository{s},
		Deliveries:        ContextDeliveryRepository{s},
		Events:            EventRepository{s},
	}
}
