package compose

import (
	agentmodel "praxis/internal/agent"
	contextmodel "praxis/internal/context"
	"praxis/internal/contracts"
	"praxis/internal/repository"
	runtimecontract "praxis/internal/runtime"

	"context"
	"errors"
)

// executionContextProvider 为非 API 的队列启动路径装配 Context，避免 runtime 依赖 service。
type executionContextProvider struct {
	contexts    repository.SessionContextRepository
	transcripts runtimecontract.TranscriptLoader
	builder     contextmodel.ContextBuilder
}

func newExecutionContextProvider(
	contexts repository.SessionContextRepository,
	transcripts runtimecontract.TranscriptLoader,
) executionContextProvider {
	return executionContextProvider{
		contexts:    contexts,
		transcripts: transcripts,
		builder:     contextmodel.NewContextBuilder(0, 0),
	}
}

func (p executionContextProvider) BuildContext(
	ctx context.Context,
	agent agentmodel.Agent,
	systemPrompt string,
	currentInput string,
) (contextmodel.BuildResult, error) {
	if ctx == nil {
		return contextmodel.BuildResult{}, errors.New("execution context build context is required")
	}
	contextRevision, err := p.contexts.CurrentRevision(ctx, agent.SessionID)
	if err != nil {
		return contextmodel.BuildResult{}, err
	}
	entries := make([]contextmodel.Entry, 0, contextRevision)
	if contextRevision > 0 {
		entries, err = p.loadContextEntries(ctx, agent.SessionID, contextRevision)
		if err != nil {
			return contextmodel.BuildResult{}, err
		}
	}
	// 空会话没有共享上下文条目，仍用首个 revision 冻结首次输入。
	buildRevision := contextRevision
	if buildRevision == 0 {
		buildRevision = 1
	}
	transcript, err := p.transcripts.LoadTranscript(ctx, agent.SessionID, agent.ID)
	if err != nil {
		return contextmodel.BuildResult{}, err
	}
	messages := make([]contextmodel.TranscriptMessage, len(transcript.Messages))
	for index, message := range transcript.Messages {
		blocks := make([]contextmodel.ContextBlock, len(message.Blocks))
		for blockIndex, block := range message.Blocks {
			blocks[blockIndex] = block.Clone()
		}
		messages[index] = contextmodel.TranscriptMessage{
			Sequence:    message.Sequence,
			ExecutionID: message.ExecutionID.String(),
			MessageID:   message.MessageID,
			Digest:      message.Digest,
			Role:        message.Role,
			Content:     message.Content,
			Blocks:      blocks,
			InputBytes:  transcriptMessageInputBytes(message),
		}
	}
	return p.builder.Build(contextmodel.BuildInput{
		SystemPrompt:              systemPrompt,
		SessionRevision:           buildRevision,
		Entries:                   entries,
		TranscriptThroughSequence: transcript.ThroughSequence,
		TranscriptMessages:        messages,
		CurrentInput:              currentInput,
	})
}

func (p executionContextProvider) loadContextEntries(
	ctx context.Context,
	sessionID contracts.SessionID,
	contextRevision uint64,
) ([]contextmodel.Entry, error) {
	entries := make([]contextmodel.Entry, 0, contextRevision)
	for after := uint64(0); after < contextRevision; {
		page, err := p.contexts.List(ctx, sessionID, after, 512)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return nil, errors.New("session context revision sequence is incomplete")
		}
		for _, entry := range page {
			if entry.Revision != uint64(len(entries)+1) {
				return nil, errors.New("session context revision sequence is incomplete")
			}
			entries = append(entries, contextmodel.Entry{
				ID:                entry.ID.String(),
				SessionID:         entry.SessionID.String(),
				Revision:          entry.Revision,
				Kind:              contextmodel.EntryKind(entry.Kind),
				SourceExecutionID: entry.SourceExecutionID.String(),
				Content:           entry.Content,
				ContentDigest:     entry.ContentDigest,
				CreatedAt:         entry.CreatedAt,
			})
		}
		after = page[len(page)-1].Revision
	}
	return entries, nil
}

func transcriptMessageInputBytes(message runtimecontract.AgentSessionMessage) int {
	size := 0
	for _, block := range message.Blocks {
		size += len(block.Input)
	}
	return size
}
