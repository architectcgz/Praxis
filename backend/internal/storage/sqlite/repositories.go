package sqlite

// Repositories contains typed views over one SQLite connection. It preserves
// the core's narrow aggregate ports without creating competing transaction or
// schema owners for the same product database. Each aggregate keeps a validated
// value as one JSON payload while exposing the identifiers and lifecycle columns
// required for transactions, uniqueness constraints, recovery, and safe read
// projections.
type Repositories struct {
	TaskSessions     TaskSessionRepository
	TaskPackets      TaskPacketRepository
	ContextManifests ContextManifestRepository
	CapabilityGrants CapabilityGrantRepository
	Delegations      DelegationRepository
	AgentThreads     AgentThreadRepository
	AgentRuns        AgentRunRepository
	WorkspaceLeases  WorkspaceLeaseRepository
	AgentResults     AgentResultRepository
	Briefings        BriefingRepository
	Deliveries       DeliveryRepository
	Notes            NoteRepository
	Events           EventRepository
}

// Repositories returns all aggregate views backed by this Store.
func (s *Store) Repositories() Repositories {
	return Repositories{
		TaskSessions:     TaskSessionRepository{s},
		TaskPackets:      TaskPacketRepository{s},
		ContextManifests: ContextManifestRepository{s},
		CapabilityGrants: CapabilityGrantRepository{s},
		Delegations:      DelegationRepository{s},
		AgentThreads:     AgentThreadRepository{s},
		AgentRuns:        AgentRunRepository{s},
		WorkspaceLeases:  WorkspaceLeaseRepository{s},
		AgentResults:     AgentResultRepository{s},
		Briefings:        BriefingRepository{s},
		Deliveries:       DeliveryRepository{s},
		Notes:            NoteRepository{s},
		Events:           EventRepository{s},
	}
}
