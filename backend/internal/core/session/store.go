package session

import (
	"context"

	"praxis/internal/core/domain"
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
	FindArtifact(context.Context, domain.AgentThreadID, string) (*ArtifactRef, error)
}

// SessionRepairer performs conservative repair before startup recovery changes
// product state. Physical paths and backup details stay inside storage.
type SessionRepairer interface {
	Repair(context.Context, domain.AgentThreadID) error
}

// SessionStore is the core-facing session maintenance port. Transcript append
// and provider context projection are runtime concerns, not core API types.
type SessionStore interface {
	ArtifactLookup
	SessionRepairer
	Close(context.Context) error
}
