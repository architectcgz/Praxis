package sqlite

// Repositories contains only aggregates whose complete state is relational
// metadata. Content-bearing repositories live in infrastructure/storage.
type Repositories struct {
	Projects   ProjectRepository
	Workspaces WorkspaceRepository
	Agents     AgentRepository
	Sessions   SessionRepository
	Controls   AgentControlCommandRepository
	Deliveries ContextDeliveryRepository
	Events     EventRepository
}

func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:   ProjectRepository{s},
		Workspaces: WorkspaceRepository{s},
		Agents:     AgentRepository{s},
		Sessions:   SessionRepository{s},
		Controls:   AgentControlCommandRepository{s},
		Deliveries: ContextDeliveryRepository{s},
		Events:     EventRepository{s},
	}
}
