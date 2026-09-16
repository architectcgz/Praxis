package workspace

import "time"

type WorkspaceLeaseState string

const (
	LeaseActive   WorkspaceLeaseState = "active"
	LeaseReleased WorkspaceLeaseState = "released"
)

type WorkspaceWriteLease struct {
	ID                    WorkspaceLeaseID
	WorkspaceID           WorkspaceID
	WorkspacePathSnapshot string
	WorkspaceRevision     uint64
	OwnerAgentID          AgentID
	GrantID               CapabilityGrantID
	State                 WorkspaceLeaseState
	AcquiredAt            time.Time
	ReleasedAt            time.Time
}

func AcquireWorkspaceWriteLease(
	id WorkspaceLeaseID,
	workspaceID WorkspaceID,
	ownerAgentID AgentID,
	grant CapabilityGrant,
	at time.Time,
) (WorkspaceWriteLease, DomainEvent, error) {
	lease := WorkspaceWriteLease{
		ID:                    id,
		WorkspaceID:           workspaceID,
		WorkspacePathSnapshot: grant.WorkspacePathSnapshot,
		WorkspaceRevision:     grant.WorkspaceRevision,
		OwnerAgentID:          ownerAgentID,
		GrantID:               grant.ID,
		State:                 LeaseActive,
		AcquiredAt:            at.UTC(),
	}
	if !grant.HasWriteAccess() {
		return WorkspaceWriteLease{}, DomainEvent{}, invalidValue(
			"workspaceLease",
			"only a write grant can acquire a lease",
		)
	}
	if lease.WorkspaceID != grant.WorkspaceID {
		return WorkspaceWriteLease{}, DomainEvent{}, invalidValue(
			"workspaceLease.workspaceID",
			"lease workspace id must match the grant",
		)
	}
	if lease.WorkspacePathSnapshot != grant.WorkspacePathSnapshot || lease.WorkspaceRevision != grant.WorkspaceRevision {
		return WorkspaceWriteLease{}, DomainEvent{}, invalidValue("workspaceLease.snapshot", "lease workspace snapshot must match the grant")
	}
	if err := lease.Validate(); err != nil {
		return WorkspaceWriteLease{}, DomainEvent{}, err
	}
	event := newDomainEvent(EventLeaseAcquired, at)
	event.AgentID = ownerAgentID
	event.WorkspaceID = lease.WorkspaceID
	return lease, event, nil
}

func (l WorkspaceWriteLease) Validate() error {
	if idIsEmpty(string(l.ID)) || idIsEmpty(string(l.WorkspaceID)) || idIsEmpty(string(l.OwnerAgentID)) || idIsEmpty(string(l.GrantID)) {
		return invalidValue("workspaceLease", "required reference is missing")
	}
	if l.WorkspacePathSnapshot == "" {
		return invalidValue("workspaceLease.workspacePathSnapshot", "workspace path snapshot is required")
	}
	if l.WorkspaceRevision == 0 {
		return invalidValue("workspaceLease.workspaceRevision", "workspace revision must be positive")
	}
	if l.State != LeaseActive && l.State != LeaseReleased {
		return invalidValue("workspaceLease.state", "unknown lease state")
	}
	if l.AcquiredAt.IsZero() || (l.State == LeaseReleased && l.ReleasedAt.IsZero()) {
		return invalidValue("workspaceLease.timestamps", "invalid lease timestamps")
	}
	return nil
}

func (l *WorkspaceWriteLease) Release(at time.Time) (DomainEvent, error) {
	if l.State != LeaseActive {
		return DomainEvent{}, invalidTransition("workspaceLease", string(l.State), string(LeaseReleased))
	}
	if at.Before(l.AcquiredAt) {
		return DomainEvent{}, invalidValue("workspaceLease.releasedAt", "release time cannot precede acquisition")
	}
	l.State, l.ReleasedAt = LeaseReleased, at.UTC()
	event := newDomainEvent(EventLeaseReleased, at)
	event.AgentID = l.OwnerAgentID
	event.WorkspaceID = l.WorkspaceID
	return event, nil
}
