package sqlite

// Repositories contains only aggregates whose complete state is relational
// metadata. Content-bearing repositories live in infrastructure/storage.
type Repositories struct {
	Projects   ProjectRepository
	Workspaces WorkspaceRepository
	Agents     SessionAgentRepository
	Sessions   SessionRepository
	Controls   AgentControlCommandRepository
}

func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:   ProjectRepository{s},
		Workspaces: WorkspaceRepository{s},
		Agents:     SessionAgentRepository{s},
		Sessions:   SessionRepository{s},
		Controls:   AgentControlCommandRepository{s},
	}
}
