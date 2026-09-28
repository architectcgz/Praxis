package context

import (
	"praxis/internal/contracts"

	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxSessionContextContentBytes = 16 * 1024

// SessionContextKind identifies the durable shared context entry category.
type SessionContextKind string

const (
	SessionContextUserMessage        SessionContextKind = "user_message"
	SessionContextAcceptedConclusion SessionContextKind = "accepted_conclusion"
	SessionContextDecision           SessionContextKind = "decision"
	SessionContextReference          SessionContextKind = "reference"
)

// SessionContextEntry is an append-only fact in the shared session context.
// Revision is the stable ordering key and is never reused.
type SessionContextEntry struct {
	ID                contracts.ContextEntryID
	SessionID         contracts.SessionID
	Revision          uint64
	Kind              SessionContextKind
	SourceExecutionID contracts.AgentExecutionID
	Content           string
	ContentDigest     string
	CreatedAt         time.Time
}

func NewSessionContextEntry(
	id contracts.ContextEntryID,
	sessionID contracts.SessionID,
	revision uint64,
	kind SessionContextKind,
	sourceExecutionID contracts.AgentExecutionID,
	content string,
	at time.Time,
) (SessionContextEntry, error) {
	content = strings.TrimSpace(content)
	entry := SessionContextEntry{
		ID: id, SessionID: sessionID, Revision: revision, Kind: kind,
		SourceExecutionID: sourceExecutionID, Content: content,
		ContentDigest: contentDigest(content), CreatedAt: at.UTC(),
	}
	if err := entry.Validate(); err != nil {
		return SessionContextEntry{}, err
	}
	return entry, nil
}

func (e SessionContextEntry) Validate() error {
	if contracts.EmptyID(string(e.ID)) || contracts.EmptyID(string(e.SessionID)) {
		return contracts.InvalidValue("sessionContext", "entry id and session id are required")
	}
	if e.Revision == 0 {
		return contracts.InvalidValue("sessionContext.revision", "revision must be positive")
	}
	switch e.Kind {
	case SessionContextUserMessage, SessionContextAcceptedConclusion, SessionContextDecision, SessionContextReference:
	default:
		return contracts.InvalidValue("sessionContext.kind", "unknown context entry kind")
	}
	if e.Content == "" || !utf8.ValidString(e.Content) || len([]byte(e.Content)) > MaxSessionContextContentBytes {
		return contracts.InvalidValue("sessionContext.content", "content is empty, invalid UTF-8, or exceeds the bounded limit")
	}
	if e.ContentDigest != contentDigest(e.Content) {
		return contracts.InvalidValue("sessionContext.contentDigest", "content digest does not match content")
	}
	if e.CreatedAt.IsZero() {
		return contracts.InvalidValue("sessionContext.createdAt", "createdAt is required")
	}
	return nil
}

func (e SessionContextEntry) Snapshot() SessionContextEntry { return e }

func contentDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return "sha256-" + hex.EncodeToString(digest[:])
}
