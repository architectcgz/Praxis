package session

import (
	"context"

	domainfoundation "praxis/internal/domain/foundation"
)

// ArtifactRef identifies an approved artifact already appended to a target
// session. It exposes only the idempotency evidence needed by orchestration.
type ArtifactRef struct {
	EntryID  string
	Sequence uint64
}

// ArtifactLookup lets orchestration reconcile a delivery without reading a
// transcript or depending on the JSONL storage representation.
type ArtifactLookup interface {
	FindArtifact(context.Context, domainfoundation.AgentID, string) (*ArtifactRef, error)
}

// SessionRepairer performs conservative repair before startup recovery changes
// product state. Physical paths and backup details stay inside storage.
type SessionRepairer interface {
	Repair(context.Context, domainfoundation.AgentID) error
}

// SessionStore is the core-facing session maintenance interface. Transcript append
// and provider context projection are runtime concerns, not core API types.
type SessionStore interface {
	ArtifactLookup
	SessionRepairer
	Close(context.Context) error
}
