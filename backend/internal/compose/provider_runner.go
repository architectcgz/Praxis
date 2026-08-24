package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"praxis/internal/agentruntime"
	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
	"praxis/internal/logging"
	"praxis/internal/providers/registry"
	"praxis/internal/storage"
	"praxis/internal/storage/agentlog"
	"praxis/internal/storage/sqlite"
)

type providerRunner struct {
	store          *sqlite.Store
	root           storage.DataRoot
	registry       *registry.Registry
	logger         *logging.Logger
	outputObserver agentruntime.AgentOutputObserver
}

func (r *providerRunner) Run(
	ctx context.Context,
	execution domain.AgentExecution,
) (domain.ExecutionOutcome, domain.ExecutionFailureCode, error) {
	if ctx == nil {
		return domain.ExecutionFailed, domain.ExecutionFailureRuntimeFailed,
			errors.New("provider runner context is required")
	}
	session, err := agentlog.Open(r.root, execution.SessionID, execution.AgentID)
	if err != nil {
		return failedRunner(err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	return r.runWithMessageStore(ctx, execution, session)
}

func (r *providerRunner) RunWithSession(
	ctx context.Context,
	execution domain.AgentExecution,
	store coresession.AgentSessionStore,
) (domain.ExecutionOutcome, domain.ExecutionFailureCode, error) {
	messages, ok := store.(providerMessageStore)
	if !ok {
		return failedRunner(errors.New("agent session does not expose provider message operations"))
	}
	return r.runWithMessageStore(ctx, execution, messages)
}

type providerMessageStore interface {
	ListMessages(context.Context, int) ([]coresession.AgentSessionMessage, error)
	AppendMessage(context.Context, domain.AgentExecutionID, string, domain.RequestID, string) error
}

func (r *providerRunner) runWithMessageStore(
	ctx context.Context,
	execution domain.AgentExecution,
	session providerMessageStore,
) (outcome domain.ExecutionOutcome, failureCode domain.ExecutionFailureCode, err error) {
	output := newProviderOutputBatcher(r.outputObserver, execution)
	defer func() {
		output.Flush()
		r.publishOutputSettled(execution)
	}()
	if ctx == nil {
		return domain.ExecutionFailed, domain.ExecutionFailureRuntimeFailed,
			errors.New("provider runner context is required")
	}
	started := time.Now()
	r.logger.Infof("provider execution started id=%s agent=%s", execution.ID, execution.AgentID)
	defer func() {
		durationMillis := time.Since(started).Milliseconds()
		r.logger.Debugf("provider execution finished id=%s duration_ms=%d", execution.ID, durationMillis)
	}()
	agent, err := r.store.GetAgent(ctx, execution.AgentID)
	if err != nil {
		return failedRunner(err)
	}
	grant, err := r.store.GetCapabilityGrant(ctx, execution.Input.CapabilityGrantID)
	if err != nil {
		return failedRunner(err)
	}
	packet, err := r.store.GetTaskPacket(ctx, execution.Input.TaskPacketID)
	if err != nil {
		return failedRunner(err)
	}
	manifest, err := r.store.GetContextManifest(ctx, execution.Input.ContextManifestID)
	if err != nil {
		return failedRunner(err)
	}
	model, err := r.registry.Model(grant.Model.ID)
	if err != nil {
		return domain.ExecutionFailed, domain.ExecutionFailureProvider, err
	}
	streamPort, err := r.registry.StreamPortFor(grant.Model)
	if err != nil {
		return domain.ExecutionFailed, domain.ExecutionFailureProvider, err
	}
	messages, err := session.ListMessages(ctx, 200)
	if err != nil {
		return failedRunner(err)
	}
	turnMessages := make([]agentruntime.TurnMessage, 0, len(messages)+1)
	for _, message := range messages {
		role := agentruntime.TurnRoleUser
		if message.Role == "assistant" {
			role = agentruntime.TurnRoleAssistant
		}
		turnMessages = append(turnMessages, agentruntime.TurnMessage{
			Role: role,
			Content: []agentruntime.TurnContentBlock{{
				Kind: agentruntime.TurnContentText,
				Text: message.Content,
			}},
		})
	}
	if len(turnMessages) == 0 {
		turnMessages = append(turnMessages, agentruntime.TurnMessage{
			Role: agentruntime.TurnRoleUser,
			Content: []agentruntime.TurnContentBlock{{
				Kind: agentruntime.TurnContentText,
				Text: packet.Goal,
			}},
		})
	}
	messageCount := len(turnMessages)
	r.logger.Infof("provider request prepared id=%s model=%s messages=%d", execution.ID, grant.Model.ID, messageCount)
	snapshot := agentruntime.TurnSnapshot{
		RunID: domain.AgentRunID(execution.ID), SessionReference: agent.RuntimeSessionRef, Messages: turnMessages,
		TaskPacket: packet, ContextManifest: manifest, SystemPrompt: manifest.Summary,
		Model:     grant.Model,
		Execution: execution.Input.Runtime, TurnNumber: 1, GrantID: grant.ID,
	}
	if estimatedSnapshotTokens(snapshot)+model.MaxOutputTokens > model.ContextWindow {
		return domain.ExecutionFailed, domain.ExecutionFailureProvider,
			errors.New("model context window exceeded")
	}
	stream, err := streamPort.Stream(ctx, agentruntime.ModelRequest{Snapshot: snapshot})
	if err != nil {
		return domain.ExecutionFailed, domain.ExecutionFailureProvider, err
	}
	r.logger.Infof("provider stream connected id=%s model=%s", execution.ID, grant.Model.ID)
	text, calls, err := collectProviderStream(ctx, stream, output.Append)
	if err != nil {
		return domain.ExecutionFailed, domain.ExecutionFailureProvider, err
	}
	r.logger.Infof("provider stream completed id=%s output_chars=%d tool_calls=%d", execution.ID, len(text), len(calls))
	if len(calls) > 0 {
		return domain.ExecutionFailed, domain.ExecutionFailureProvider,
			errors.New("provider requested a tool call but no tool runner is configured")
	}
	if strings.TrimSpace(text) != "" {
		if err := session.AppendMessage(ctx, execution.ID, "assistant", execution.RequestID, text); err != nil {
			return failedRunner(err)
		}
	}
	return domain.ExecutionCompleted, "", nil
}

func estimatedSnapshotTokens(snapshot agentruntime.TurnSnapshot) int {
	characters := len(snapshot.SystemPrompt)
	for _, message := range snapshot.Messages {
		for _, block := range message.Content {
			characters += len(block.Text)
		}
	}
	return characters / 4
}

func collectProviderStream(
	ctx context.Context,
	stream <-chan agentruntime.ModelStreamEvent,
	onTextDelta func(string),
) (string, []agentruntime.ToolCall, error) {
	var text strings.Builder
	calls := make([]agentruntime.ToolCall, 0)
	for {
		select {
		case <-ctx.Done():
			return text.String(), calls, ctx.Err()
		case event, ok := <-stream:
			if !ok {
				return text.String(), calls, nil
			}
			switch event.Kind {
			case agentruntime.StreamTextDelta:
				text.WriteString(event.Text)
				if onTextDelta != nil && event.Text != "" {
					onTextDelta(event.Text)
				}
			case agentruntime.StreamToolCall:
				calls = append(calls, event.ToolCall)
			case agentruntime.StreamComplete:
				return text.String(), calls, nil
			case agentruntime.StreamError:
				if event.Err != nil {
					return text.String(), calls, event.Err
				}
				return text.String(), calls, errors.New("provider stream failed")
			default:
				return text.String(), calls, errors.New("provider stream emitted an unknown event")
			}
		}
	}
}

const (
	outputFlushInterval = 20 * time.Millisecond
	outputFlushChars    = 48
)

// providerOutputBatcher limits cross-bridge updates while preserving the
// provider's text order. The full response is persisted independently.
type providerOutputBatcher struct {
	observer  agentruntime.AgentOutputObserver
	execution domain.AgentExecution
	text      strings.Builder
	lastFlush time.Time
}

func newProviderOutputBatcher(
	observer agentruntime.AgentOutputObserver,
	execution domain.AgentExecution,
) *providerOutputBatcher {
	return &providerOutputBatcher{observer: observer, execution: execution}
}

func (b *providerOutputBatcher) Append(text string) {
	if b == nil || b.observer == nil || text == "" {
		return
	}
	b.text.WriteString(text)
	if b.text.Len() >= outputFlushChars || time.Since(b.lastFlush) >= outputFlushInterval {
		b.Flush()
	}
}

func (b *providerOutputBatcher) Flush() {
	if b == nil || b.observer == nil || b.text.Len() == 0 {
		return
	}
	b.observer(agentruntime.AgentOutputEvent{
		Kind:        agentruntime.AgentOutputTextDelta,
		AgentID:     b.execution.AgentID,
		ExecutionID: b.execution.ID,
		Text:        b.text.String(),
	})
	b.text.Reset()
	b.lastFlush = time.Now()
}

func (r *providerRunner) publishOutputSettled(execution domain.AgentExecution) {
	if r.outputObserver == nil {
		return
	}
	r.outputObserver(agentruntime.AgentOutputEvent{
		Kind:        agentruntime.AgentOutputSettled,
		AgentID:     execution.AgentID,
		ExecutionID: execution.ID,
	})
}

func failedRunner(err error) (domain.ExecutionOutcome, domain.ExecutionFailureCode, error) {
	if err == nil {
		err = errors.New("provider runner failed")
	}
	return domain.ExecutionFailed, domain.ExecutionFailureRuntimeFailed, fmt.Errorf("provider runner: %w", err)
}
