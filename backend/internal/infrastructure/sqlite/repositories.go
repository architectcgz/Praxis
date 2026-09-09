package sqlite

// Repositories contains repositories for durable supporting aggregates. The
// orchestration repositories are exposed separately so composition can wire a
// single, explicit target model.
type Repositories struct {
	Projects        ProjectRepository
	Workspaces      WorkspaceRepository
	Delegations     DelegationRepository
	WorkspaceLeases WorkspaceLeaseRepository
	AgentResults    AgentResultRepository
	Briefings       BriefingRepository
	Notes           NoteRepository
	Events          EventRepository
}

func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:        ProjectRepository{s},
		Workspaces:      WorkspaceRepository{s},
		Delegations:     DelegationRepository{s},
		WorkspaceLeases: WorkspaceLeaseRepository{s},
		AgentResults:    AgentResultRepository{s},
		Briefings:       BriefingRepository{s},
		Notes:           NoteRepository{s},
		Events:          EventRepository{s},
	}
}
