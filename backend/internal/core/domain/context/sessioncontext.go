package context

import "time"

// SessionContextKind identifies the durable shared context entry category.
type SessionContextKind string

const (
	SessionContextUserMessage        SessionContextKind = "user_message"
	SessionContextAgentMessage       SessionContextKind = "agent_message"
	SessionContextAcceptedConclusion SessionContextKind = "accepted_conclusion"
	SessionContextDecision           SessionContextKind = "decision"
	SessionContextReference          SessionContextKind = "reference"
)

// SessionContextEntry is an append-only fact in the shared session context.
// Revision is the stable ordering key and is never reused.
type SessionContextEntry struct {
	SessionID         SessionID
	Revision          uint64
	Kind              SessionContextKind
	SourceExecutionID AgentExecutionID
	Content           string
	CreatedAt         time.Time
}

func NewSessionContextEntry(
	sessionID SessionID,
	revision uint64,
	kind SessionContextKind,
	sourceExecutionID AgentExecutionID,
	content string,
	at time.Time,
) (SessionContextEntry, error) {
	entry := SessionContextEntry{
		SessionID: sessionID, Revision: revision, Kind: kind,
		SourceExecutionID: sourceExecutionID, Content: content, CreatedAt: at.UTC(),
	}
	if err := entry.Validate(); err != nil {
		return SessionContextEntry{}, err
	}
	return entry, nil
}

func (e SessionContextEntry) Validate() error {
	if idIsEmpty(string(e.SessionID)) {
		return invalidValue("sessionContext.sessionID", "session id is required")
	}
	if e.Revision == 0 {
		return invalidValue("sessionContext.revision", "revision must be positive")
	}
	switch e.Kind {
	case SessionContextUserMessage, SessionContextAgentMessage,
		SessionContextAcceptedConclusion, SessionContextDecision, SessionContextReference:
	default:
		return invalidValue("sessionContext.kind", "unknown context entry kind")
	}
	if e.Content == "" {
		return invalidValue("sessionContext.content", "content is required")
	}
	if e.CreatedAt.IsZero() {
		return invalidValue("sessionContext.createdAt", "createdAt is required")
	}
	return nil
}

func (e SessionContextEntry) Snapshot() SessionContextEntry { return e }
