package session

import (
	"context"
	"encoding/json"
	"time"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
)

// AgentSessionMessage is the UI-safe projection of one durable transcript
// message. Storage-specific JSONL fields remain private to the adapter.
type AgentSessionMessage struct {
	Sequence    uint64
	At          time.Time
	ExecutionID domainfoundation.AgentExecutionID
	MessageID   string
	Digest      string
	Role        string
	Content     string
	// Blocks contains the complete provider-neutral turn representation.
	Blocks []TranscriptContentBlock
}

// TranscriptContentBlock is the provider-neutral durable representation of a
// message block. It contains only data required to reconstruct a model turn.
type TranscriptContentBlock struct {
	Kind       string
	Text       string
	ToolCallID string
	ToolName   string
	Input      json.RawMessage
	IsError    bool
}

// AgentSessionHeader is the immutable identity record for one Agent-owned
// transcript. The Session never directly owns a shared transcript.
type AgentSessionHeader struct {
	SessionID        domainfoundation.SessionID
	AgentID          domainfoundation.AgentID
	WorkspaceID      domainfoundation.WorkspaceID
	Profile          domainsecurity.AgentProfile
	InjectionNonce   string
	MinReaderVersion uint16
	WrittenBy        string
}

type ExecutionStartReceipt struct {
	ExecutionID domainfoundation.AgentExecutionID
	RequestID   domainfoundation.RequestID
	EntryID     string
	Sequence    uint64
	InputDigest string
}

type ExecutionSettlementReceipt struct {
	ExecutionID domainfoundation.AgentExecutionID
	RequestID   domainfoundation.RequestID
	EntryID     string
	Sequence    uint64
	Outcome     domainexecution.ExecutionOutcome
	FailureCode domainexecution.ExecutionFailureCode
}

type ContextArtifact struct {
	DeliveryID domainfoundation.DeliveryID
	Kind       string
	ArtifactID string
	Body       json.RawMessage
}

type ContextArtifactReceipt struct {
	DeliveryID domainfoundation.DeliveryID
	EntryID    string
	Sequence   uint64
}

// TranscriptReceiptStore is the transcript-side interface used for cross-store receipt
// reconciliation. Product state remains exclusively in application services.
type TranscriptReceiptStore interface {
	Initialize(context.Context, AgentSessionHeader) error
	AppendExecutionStart(context.Context, domainexecution.AgentExecution) (ExecutionStartReceipt, error)
	AppendExecutionSettlement(context.Context, ExecutionSettlementReceipt) (ExecutionSettlementReceipt, error)
	AppendContextArtifact(context.Context, ContextArtifact) (ContextArtifactReceipt, error)
	FindExecutionStart(context.Context, domainfoundation.AgentExecutionID) (*ExecutionStartReceipt, error)
	FindExecutionSettlement(context.Context, domainfoundation.AgentExecutionID) (*ExecutionSettlementReceipt, error)
	FindContextArtifact(context.Context, domainfoundation.DeliveryID) (*ContextArtifactReceipt, error)
	Repair(context.Context) (bool, error)
	Close(context.Context) error
}

// TranscriptMessageStore is the runtime-facing message port. It is separate
// from receipt reconciliation so callers that only need lifecycle receipts do
// not gain access to transcript content.
type TranscriptMessageStore interface {
	ListMessages(context.Context, int) ([]AgentSessionMessage, error)
	ListExecutionMessages(context.Context, []domainexecution.TranscriptMessageRef, domainfoundation.AgentExecutionID) ([]AgentSessionMessage, error)
	AppendStructuredMessage(context.Context, domainfoundation.AgentExecutionID, string, string, domainfoundation.RequestID, []TranscriptContentBlock) error
}

// AgentContextArtifact is an immutable artifact selected from the Agent transcript.
type AgentContextArtifact struct {
	EntryID    string
	Sequence   uint64
	DeliveryID domainfoundation.DeliveryID
	Kind       string
	ArtifactID string
	Body       json.RawMessage
}

// TranscriptContextStore exposes only explicitly selected context artifacts.
type TranscriptContextStore interface {
	ListContextArtifacts(context.Context, []string) ([]AgentContextArtifact, error)
}

// AgentTranscriptSnapshot is the durable input boundary captured for one execution.
type AgentTranscriptSnapshot struct {
	ThroughSequence uint64
	Messages        []AgentSessionMessage
	Artifacts       []AgentContextArtifact
}
