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

// SessionContextKind 标识 Session 共享上下文的事实类别。
type SessionContextKind string

const (
	SessionContextUserMessage        SessionContextKind = "user_message"
	SessionContextAcceptedConclusion SessionContextKind = "accepted_conclusion"
	SessionContextDecision           SessionContextKind = "decision"
	SessionContextReference          SessionContextKind = "reference"
)

// SessionContextEntry 是只追加的 Session 共享事实，不保存消息副本。
type SessionContextEntry struct {
	ID            contracts.ContextEntryID
	SessionID     contracts.SessionID
	Kind          SessionContextKind
	SourceTaskID  contracts.TaskID
	Content       string
	ContentDigest string
	CreatedAt     time.Time
}

func NewSessionContextEntry(
	id contracts.ContextEntryID,
	sessionID contracts.SessionID,
	kind SessionContextKind,
	sourceTaskID contracts.TaskID,
	content string,
	at time.Time,
) (SessionContextEntry, error) {
	content = strings.TrimSpace(content)
	entry := SessionContextEntry{
		ID: id, SessionID: sessionID, Kind: kind,
		SourceTaskID: sourceTaskID, Content: content,
		ContentDigest: contentDigest(content), CreatedAt: at.UTC(),
	}
	if err := entry.Validate(); err != nil {
		return SessionContextEntry{}, err
	}
	return entry, nil
}

func (e SessionContextEntry) Validate() error {
	if e.Content != strings.TrimSpace(e.Content) {
		return contracts.InvalidValue("sessionContext.content", "content must be normalized")
	}
	return e.validate()
}

func (e SessionContextEntry) validate() error {
	if contracts.EmptyID(string(e.ID)) || contracts.EmptyID(string(e.SessionID)) {
		return contracts.InvalidValue("sessionContext", "entry id and session id are required")
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

func contentDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return "sha256-" + hex.EncodeToString(digest[:])
}
