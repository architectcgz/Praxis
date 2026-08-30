package sqlite

// Repositories contains repositories for durable supporting aggregates. The
// orchestration repositories are exposed separately so composition can wire a
// single, explicit target model.
type Repositories struct {
	Projects         ProjectRepository
	Workspaces       WorkspaceRepository
	TaskPackets      TaskPacketRepository
	ContextManifests ContextManifestRepository
	CapabilityGrants CapabilityGrantRepository
	Delegations      DelegationRepository
	WorkspaceLeases  WorkspaceLeaseRepository
	AgentResults     AgentResultRepository
	Briefings        BriefingRepository
	Notes            NoteRepository
	Events           EventRepository
}

func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:         ProjectRepository{s},
		Workspaces:       WorkspaceRepository{s},
		TaskPackets:      TaskPacketRepository{s},
		ContextManifests: ContextManifestRepository{s},
		CapabilityGrants: CapabilityGrantRepository{s},
		Delegations:      DelegationRepository{s},
		WorkspaceLeases:  WorkspaceLeaseRepository{s},
		AgentResults:     AgentResultRepository{s},
		Briefings:        BriefingRepository{s},
		Notes:            NoteRepository{s},
		Events:           EventRepository{s},
	}
}
