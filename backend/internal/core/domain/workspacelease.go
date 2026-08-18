package domain

import (
	"path/filepath"
	"strings"
	"time"
)

type WorkspaceLeaseState string

const (
	LeaseActive   WorkspaceLeaseState = "active"
	LeaseReleased WorkspaceLeaseState = "released"
)

type WorkspaceWriteLease struct {
	ID            WorkspaceLeaseID
	WorkspaceKey  string
	OwnerThreadID AgentThreadID
	GrantID       CapabilityGrantID
	State         WorkspaceLeaseState
	AcquiredAt    time.Time
	ReleasedAt    time.Time
}

func AcquireWorkspaceWriteLease(id WorkspaceLeaseID, workspaceKey string, ownerThreadID AgentThreadID, grant CapabilityGrant, at time.Time) (WorkspaceWriteLease, DomainEvent, error) {
	lease := WorkspaceWriteLease{
		ID:            id,
		WorkspaceKey:  filepath.Clean(strings.TrimSpace(workspaceKey)),
		OwnerThreadID: ownerThreadID,
		GrantID:       grant.ID,
		State:         LeaseActive,
		AcquiredAt:    at.UTC(),
	}
	if !grant.HasWriteAccess() {
		return WorkspaceWriteLease{}, DomainEvent{}, invalidValue("workspaceLease", "only a write grant can acquire a lease")
	}
	if lease.WorkspaceKey != grant.WorkspaceKey {
		return WorkspaceWriteLease{}, DomainEvent{}, invalidValue("workspaceLease.workspaceKey", "lease workspace must match the grant")
	}
	if err := lease.Validate(); err != nil {
		return WorkspaceWriteLease{}, DomainEvent{}, err
	}
	event := newDomainEvent(EventLeaseAcquired, at)
	event.AgentThread = ownerThreadID
	event.Payload = map[string]string{"workspaceKey": lease.WorkspaceKey}
	return lease, event, nil
}

func (l WorkspaceWriteLease) Validate() error {
	if idIsEmpty(string(l.ID)) || idIsEmpty(string(l.OwnerThreadID)) || idIsEmpty(string(l.GrantID)) {
		return invalidValue("workspaceLease", "required reference is missing")
	}
	if l.WorkspaceKey == "." || !filepath.IsAbs(l.WorkspaceKey) {
		return invalidValue("workspaceLease.workspaceKey", "workspace key must be an absolute path")
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
	event.AgentThread = l.OwnerThreadID
	event.Payload = map[string]string{"workspaceKey": l.WorkspaceKey}
	return event, nil
}
