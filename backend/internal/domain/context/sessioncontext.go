package context

import (
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
	ID                ContextEntryID
	SessionID         SessionID
	Revision          uint64
	Kind              SessionContextKind
	SourceExecutionID AgentExecutionID
	Content           string
	ContentDigest     string
	CreatedAt         time.Time
}

func NewSessionContextEntry(
	id ContextEntryID,
	sessionID SessionID,
	revision uint64,
	kind SessionContextKind,
	sourceExecutionID AgentExecutionID,
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
	if idIsEmpty(string(e.ID)) || idIsEmpty(string(e.SessionID)) {
		return invalidValue("sessionContext", "entry id and session id are required")
	}
	if e.Revision == 0 {
		return invalidValue("sessionContext.revision", "revision must be positive")
	}
	switch e.Kind {
	case SessionContextUserMessage, SessionContextAcceptedConclusion, SessionContextDecision, SessionContextReference:
	default:
		return invalidValue("sessionContext.kind", "unknown context entry kind")
	}
	if e.Content == "" || !utf8.ValidString(e.Content) || len([]byte(e.Content)) > MaxSessionContextContentBytes {
		return invalidValue("sessionContext.content", "content is empty, invalid UTF-8, or exceeds the bounded limit")
	}
	if e.ContentDigest != contentDigest(e.Content) {
		return invalidValue("sessionContext.contentDigest", "content digest does not match content")
	}
	if e.CreatedAt.IsZero() {
		return invalidValue("sessionContext.createdAt", "createdAt is required")
	}
	return nil
}

func (e SessionContextEntry) Snapshot() SessionContextEntry { return e }

func contentDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return "sha256-" + hex.EncodeToString(digest[:])
}
