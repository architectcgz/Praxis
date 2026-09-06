package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainexecution "praxis/internal/domain/execution"
	domainsecurity "praxis/internal/domain/security"

	runtimecontract "praxis/internal/runtime"
	sessionport "praxis/internal/session"
	"praxis/internal/system"
)

type ExecutionModel struct {
	Stream          ModelStream
	ContextWindow   int
	MaxOutputTokens int
}

type ExecutionModelResolver interface {
	ResolveExecutionModel(domainsecurity.ModelSelection) (ExecutionModel, error)
}

type ExecutionEngineConfig struct {
	Models         ExecutionModelResolver
	Tools          ToolCatalog
	ToolInvoker    ToolInvoker
	OutputObserver AgentOutputObserver
	Clock          system.Clock
	Logf           func(string, ...any)
}

type ExecutionEngine struct {
	models         ExecutionModelResolver
	tools          ToolCatalog
	toolInvoker    ToolInvoker
	outputObserver AgentOutputObserver
	clock          system.Clock
	logf           func(string, ...any)
}

func NewExecutionEngine(config ExecutionEngineConfig) (*ExecutionEngine, error) {
	if config.Models == nil {
		return nil, errors.New("execution model resolver is required")
	}
	logf := config.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	clock := config.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	return &ExecutionEngine{
		models:         config.Models,
		tools:          config.Tools,
		toolInvoker:    config.ToolInvoker,
		outputObserver: config.OutputObserver,
		clock:          clock,
		logf:           logf,
	}, nil
}

func (e *ExecutionEngine) RunWithSession(ctx context.Context, execution domainexecution.AgentExecution, store sessionport.TranscriptReceiptStore) (domainexecution.ExecutionOutcome, domainexecution.ExecutionFailureCode, error) {
	if store == nil {
		return failedExecution(errors.New("transcript store is required"))
	}
	messages, ok := store.(sessionport.TranscriptMessageStore)
	if !ok {
		return failedExecution(errors.New("transcript does not implement the target message port"))
	}
	return e.run(ctx, execution, messages)
}

func (e *ExecutionEngine) run(ctx context.Context, execution domainexecution.AgentExecution, transcript sessionport.TranscriptMessageStore) (domainexecution.ExecutionOutcome, domainexecution.ExecutionFailureCode, error) {
	output := newOutputBatcher(e.outputObserver, execution, e.clock)
	defer output.Flush()
	if ctx == nil {
		return failedExecution(errors.New("execution engine context is required"))
	}
	grant := execution.Input.Security.CapabilityGrant
	model, err := e.models.ResolveExecutionModel(grant.Model)
	if err != nil {
		return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureProvider, err
	}
	if model.Stream == nil {
		return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureProviderUnavailable, errors.New("execution model stream is unavailable")
	}
	messages, err := transcript.ListMessages(ctx, 0)
	if err != nil {
		return failedExecution(err)
	}
	turnMessages := transcriptTurns(messages)
	if len(turnMessages) == 0 {
		return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureStorage,
			&RuntimeError{Code: ErrorStorage, Message: "execution transcript has no messages"}
	}
	limits := grant.ResourceLimits
	maxTurns := limits.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 32
	}
	maxToolCalls := limits.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = 128
	}
	var outputBytes int64
	var toolCalls int
	for turn := 1; turn <= maxTurns; turn++ {
		snapshot := TurnSnapshot{
			ExecutionID: execution.ID, SessionReference: execution.SessionID.String(), Messages: runtimecontract.CloneTurnMessages(turnMessages),
			ContextManifest:  execution.Input.ContextManifest,
			ContextSelection: execution.Input.ContextSelection, SystemPrompt: execution.Input.ContextManifest.Summary,
			Model: grant.Model, Tools: toolDefinitions(grant, e.tools), Execution: execution.Input.Runtime, TurnNumber: turn, GrantID: grant.ID,
		}
		if limits.MaxInputBytes > 0 && turnInputBytes(snapshot) > limits.MaxInputBytes {
			return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureResourceLimit, &RuntimeError{Code: ErrorResourceLimit, Message: "execution input limit exceeded"}
		}
		if model.ContextWindow > 0 && estimatedTokens(snapshot)+model.MaxOutputTokens > model.ContextWindow {
			return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureResourceLimit, &RuntimeError{Code: ErrorResourceLimit, Message: "model context window exceeded"}
		}
		e.logf("provider request prepared id=%s model=%s messages=%d turn=%d", execution.ID, grant.Model.ModelID, len(turnMessages), turn)
		stream, err := model.Stream.Stream(ctx, ModelRequest{Snapshot: snapshot})
		if err != nil {
			return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureProvider, err
		}
		text, calls, err := collectStream(ctx, stream, output.Append)
		if err != nil {
			return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureProvider, err
		}
		outputBytes += int64(len([]byte(text)))
		if limits.MaxOutputBytes > 0 && outputBytes > limits.MaxOutputBytes {
			return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureResourceLimit, &RuntimeError{Code: ErrorResourceLimit, Message: "execution output limit exceeded"}
		}
		assistantBlocks := make([]TurnContentBlock, 0, len(calls)+1)
		if strings.TrimSpace(text) != "" {
			assistantBlocks = append(assistantBlocks, TurnContentBlock{Kind: TurnContentText, Text: text})
		}
		for _, call := range calls {
			assistantBlocks = append(assistantBlocks, TurnContentBlock{Kind: TurnContentToolUse, ToolCallID: call.ID, ToolName: string(call.Name), Input: append([]byte(nil), call.Snapshot().Input...)})
		}
		if len(assistantBlocks) > 0 {
			if err := transcript.AppendStructuredMessage(ctx, execution.ID, fmt.Sprintf("assistant:%d", turn), "assistant", execution.RequestID, transcriptBlocks(assistantBlocks)); err != nil {
				return failedExecution(err)
			}
			turnMessages = append(turnMessages, TurnMessage{Role: TurnRoleAssistant, Content: assistantBlocks})
		}
		if len(calls) == 0 {
			return domainexecution.ExecutionCompleted, "", nil
		}
		if e.toolInvoker == nil {
			return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureTool,
				&RuntimeError{Code: ErrorTool, Message: "tool capability is unavailable"}
		}
		if toolCalls+len(calls) > maxToolCalls {
			return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureResourceLimit,
				&RuntimeError{Code: ErrorResourceLimit, Message: "execution tool call limit exceeded"}
		}
		for callIndex, rawCall := range calls {
			call := rawCall.Snapshot()
			if strings.TrimSpace(call.ID) == "" || !call.Name.Valid() || (len(call.Input) > 0 && !json.Valid(call.Input)) {
				return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureContract,
					&RuntimeError{Code: ErrorContract, Message: "provider emitted an invalid tool call"}
			}
			result, err := e.toolInvoker.Invoke(
				ctx,
				call,
				ToolInvocationContext{
					ExecutionID: execution.ID,
					SessionID:   execution.SessionID,
					AgentID:     execution.AgentID,
					Grant:       grant,
					Execution:   execution.Input.Runtime,
				},
			)
			if err != nil {
				// Persist a bounded tool-result receipt before settling failure so
				// recovery can account for the attempted side effect.
				if receiptErr := transcript.AppendStructuredMessage(
					ctx, execution.ID, fmt.Sprintf("tool-result:%d:%d:%s", turn, callIndex, call.ID),
					"tool", execution.RequestID, transcriptBlocks([]TurnContentBlock{{
						Kind: TurnContentToolResult, ToolCallID: call.ID, ToolName: string(call.Name),
						Text: "tool execution failed", IsError: true,
					}}),
				); receiptErr != nil {
					return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureStorage, &RuntimeError{Code: ErrorStorage, Message: "tool result receipt failed", Cause: receiptErr}
				}
				return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureTool,
					&RuntimeError{Code: ErrorTool, Message: "tool execution failed", Cause: err}
			}
			toolCalls++
			resultText := result.Content
			if resultText == "" {
				resultText = result.Output
			}
			if resultText == "" {
				resultText = "(empty tool result)"
			}
			if limits.MaxOutputBytes > 0 && outputBytes+int64(len([]byte(resultText))) > limits.MaxOutputBytes {
				return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureResourceLimit,
					&RuntimeError{Code: ErrorResourceLimit, Message: "execution output limit exceeded"}
			}
			outputBytes += int64(len([]byte(resultText)))
			resultBlock := TurnContentBlock{Kind: TurnContentToolResult, ToolCallID: call.ID, ToolName: string(call.Name), Text: resultText, IsError: result.ErrorClass != ""}
			if err := transcript.AppendStructuredMessage(
				ctx, execution.ID, fmt.Sprintf("tool-result:%d:%d:%s", turn, callIndex, call.ID),
				"tool", execution.RequestID, transcriptBlocks([]TurnContentBlock{resultBlock}),
			); err != nil {
				return failedExecution(err)
			}
			turnMessages = append(turnMessages, TurnMessage{
				Role: TurnRoleTool, Content: []TurnContentBlock{resultBlock},
			})
		}
	}
	return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureResourceLimit, &RuntimeError{Code: ErrorResourceLimit, Message: "execution turn limit exceeded"}
}

func transcriptTurns(messages []sessionport.AgentSessionMessage) []TurnMessage {
	turns := make([]TurnMessage, 0, len(messages))
	for _, message := range messages {
		role := TurnMessageRole(message.Role)
		if role != TurnRoleUser && role != TurnRoleAssistant && role != TurnRoleTool {
			role = TurnRoleUser
		}
		blocks := make([]TurnContentBlock, 0, len(message.Blocks))
		for _, block := range message.Blocks {
			blocks = append(blocks, TurnContentBlock{Kind: TurnContentBlockKind(block.Kind), Text: block.Text, ToolCallID: block.ToolCallID, ToolName: block.ToolName, Input: append([]byte(nil), block.Input...), IsError: block.IsError})
		}
		if len(blocks) == 0 {
			continue
		}
		turns = append(turns, TurnMessage{Role: role, Content: blocks})
	}
	return turns
}

func transcriptBlocks(blocks []TurnContentBlock) []sessionport.TranscriptContentBlock {
	result := make([]sessionport.TranscriptContentBlock, len(blocks))
	for i, block := range blocks {
		result[i] = sessionport.TranscriptContentBlock{Kind: string(block.Kind), Text: block.Text, ToolCallID: block.ToolCallID, ToolName: block.ToolName, Input: append([]byte(nil), block.Input...), IsError: block.IsError}
	}
	return result
}

func toolDefinitions(grant domainsecurity.CapabilityGrant, catalog ToolCatalog) []ToolDefinition {
	if catalog == nil {
		return nil
	}
	definitions := make([]ToolDefinition, 0, len(grant.AllowedTools))
	for _, name := range grant.AllowedTools {
		if definition, ok := catalog.Definition(name); ok {
			definitions = append(definitions, definition.Snapshot())
		}
	}
	return definitions
}

func collectStream(ctx context.Context, stream <-chan ModelStreamEvent, onTextDelta func(string)) (string, []ToolCall, error) {
	var text strings.Builder
	calls := make([]ToolCall, 0)
	for {
		select {
		case <-ctx.Done():
			return text.String(), calls, ctx.Err()
		case event, ok := <-stream:
			if !ok {
				return text.String(), calls, nil
			}
			switch event.Kind {
			case StreamTextDelta:
				text.WriteString(event.Text)
				if onTextDelta != nil && event.Text != "" {
					onTextDelta(event.Text)
				}
			case StreamToolCall:
				calls = append(calls, event.ToolCall)
			case StreamComplete:
				return text.String(), calls, nil
			case StreamError:
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

func estimatedTokens(snapshot TurnSnapshot) int {
	characters := len(snapshot.SystemPrompt)
	for _, message := range snapshot.Messages {
		for _, block := range message.Content {
			characters += len(block.Text)
		}
	}
	return characters / 4
}

func turnInputBytes(snapshot TurnSnapshot) int64 {
	var total int64
	total += int64(len([]byte(snapshot.SystemPrompt)))
	for _, message := range snapshot.Messages {
		for _, block := range message.Content {
			total += int64(len([]byte(block.Text)))
			total += int64(len(block.Input))
		}
	}
	return total
}

const (
	outputFlushInterval = 20 * time.Millisecond
	outputFlushChars    = 48
)

type outputBatcher struct {
	observer  AgentOutputObserver
	execution domainexecution.AgentExecution
	clock     system.Clock
	text      strings.Builder
	lastFlush time.Time
}

func newOutputBatcher(observer AgentOutputObserver, execution domainexecution.AgentExecution, clock system.Clock) *outputBatcher {
	return &outputBatcher{observer: observer, execution: execution, clock: clock}
}

func (b *outputBatcher) Append(value string) {
	if b == nil || b.observer == nil || value == "" {
		return
	}
	b.text.WriteString(value)
	if b.text.Len() >= outputFlushChars || b.clock.Now().Sub(b.lastFlush) >= outputFlushInterval {
		b.Flush()
	}
}

func (b *outputBatcher) Flush() {
	if b == nil || b.observer == nil || b.text.Len() == 0 {
		return
	}
	b.observer(AgentOutputEvent{Kind: AgentOutputTextDelta, AgentID: b.execution.AgentID, ExecutionID: b.execution.ID, Text: b.text.String()})
	b.text.Reset()
	b.lastFlush = b.clock.Now()
}

func failedExecution(err error) (domainexecution.ExecutionOutcome, domainexecution.ExecutionFailureCode, error) {
	if err == nil {
		err = errors.New("execution engine failed")
	}
	return domainexecution.ExecutionFailed, domainexecution.ExecutionFailureRuntimeFailed, fmt.Errorf("execution engine: %w", err)
}
