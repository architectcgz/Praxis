package runtime

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"time"

	appcontext "praxis/internal/context"
)

// AgentSessionMessage 是持久化 transcript 的 UI 安全投影。
type AgentSessionMessage struct {
	Sequence    uint64
	At          time.Time
	ExecutionID contracts.AgentExecutionID
	MessageID   string
	Digest      string
	Role        string
	Content     string
	Thinking    string
	Blocks      []appcontext.ContextBlock
}

// AgentSessionHeader 是一个 Agent transcript 的不可变身份记录。
type AgentSessionHeader struct {
	SessionID        contracts.SessionID
	AgentID          contracts.AgentID
	DefinitionID     contracts.AgentDefinitionID
	WorkspaceID      contracts.WorkspaceID
	Profile          contracts.AgentProfile
	InjectionNonce   string
	MinReaderVersion uint16
	WrittenBy        string
}

// ExecutionSettlementReceipt 是 execution settlement 的持久化回执。
type ExecutionSettlementReceipt struct {
	ExecutionID contracts.AgentExecutionID
	RequestID   contracts.RequestID
	EntryID     string
	Sequence    uint64
	Outcome     executionmodel.ExecutionOutcome
	FailureCode contracts.ExecutionFailureCode
}

// TranscriptReceiptStore 提供 execution 生命周期回执写入。
type TranscriptReceiptStore interface {
	Initialize(context.Context, AgentSessionHeader) error
	AppendExecutionStart(context.Context, executionmodel.AgentExecution) (ExecutionStartReceipt, error)
	AppendExecutionSettlement(context.Context, ExecutionSettlementReceipt) (ExecutionSettlementReceipt, error)
	Close(context.Context) error
}

// TranscriptMessageStore 提供 execution 使用的 transcript 消息能力。
type TranscriptMessageStore interface {
	ListMessages(context.Context, int) ([]AgentSessionMessage, error)
	ListExecutionMessages(context.Context, contracts.AgentExecutionID) ([]AgentSessionMessage, error)
	AppendStructuredMessage(context.Context, contracts.AgentExecutionID, string, string, contracts.RequestID, []appcontext.ContextBlock, ...string) error
}

// TranscriptStore 是一个 Agent execution 所需的完整 transcript port。
type TranscriptStore interface {
	TranscriptReceiptStore
	TranscriptMessageStore
}

// AgentTranscript 是一次 transcript 读取返回的消息及其 sequence 边界。
type AgentTranscript struct {
	ThroughSequence uint64
	Messages        []AgentSessionMessage
}

// TranscriptLoader 提供 Session Context 构建所需的 Agent transcript。
type TranscriptLoader interface {
	LoadTranscript(context.Context, contracts.SessionID, contracts.AgentID) (AgentTranscript, error)
}
