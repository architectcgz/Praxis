package session

import (
	"context"
	"encoding/json"
	"time"

	"praxis/internal/core/domain"
)

// AgentSessionMessage is the UI-safe projection of one durable transcript
// message. Storage-specific JSONL fields remain private to the adapter.
type AgentSessionMessage struct {
	Sequence    uint64
	At          time.Time
	ExecutionID domain.AgentExecutionID
	Role        string
	Content     string
}

// AgentSessionHeader is the immutable identity record for one Agent-owned
// transcript. The Session never directly owns a shared transcript.
type AgentSessionHeader struct {
	SessionID        domain.SessionID
	AgentID          domain.AgentID
	Profile          domain.AgentProfile
	WorkspaceKey     string
	InjectionNonce   string
	MinReaderVersion uint16
	WrittenBy        string
}

type ExecutionStartReceipt struct {
	ExecutionID domain.AgentExecutionID
	RequestID   domain.RequestID
	EntryID     string
	Sequence    uint64
	InputDigest string
}

type ExecutionSettlementReceipt struct {
	ExecutionID domain.AgentExecutionID
	EntryID     string
	Sequence    uint64
	Outcome     domain.ExecutionOutcome
	FailureCode domain.ExecutionFailureCode
}

type ContextArtifact struct {
	DeliveryID domain.DeliveryID
	Kind       string
	ArtifactID string
	Body       json.RawMessage
}

type ContextArtifactReceipt struct {
	DeliveryID domain.DeliveryID
	EntryID    string
	Sequence   uint64
}

// AgentSessionStore is the transcript-side port used for cross-store receipt
// reconciliation. Product state remains exclusively in AgentOrchestrator.
type AgentSessionStore interface {
	Initialize(context.Context, AgentSessionHeader) error
	AppendExecutionStart(context.Context, domain.AgentExecution) (ExecutionStartReceipt, error)
	AppendExecutionSettlement(context.Context, ExecutionSettlementReceipt) (ExecutionSettlementReceipt, error)
	AppendContextArtifact(context.Context, ContextArtifact) (ContextArtifactReceipt, error)
	FindExecutionStart(context.Context, domain.AgentExecutionID) (*ExecutionStartReceipt, error)
	FindExecutionSettlement(context.Context, domain.AgentExecutionID) (*ExecutionSettlementReceipt, error)
	FindContextArtifact(context.Context, domain.DeliveryID) (*ContextArtifactReceipt, error)
	Repair(context.Context) (bool, error)
	Close(context.Context) error
}
