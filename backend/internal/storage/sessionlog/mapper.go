package sessionlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"praxis/internal/agentruntime"
)

// manifestToEntry is the anti-corruption boundary between session metadata and
// the versioned JSONL schema. The header is not a runtime event.
func manifestToEntry(manifest agentruntime.SessionManifest, occurredAt time.Time) (SessionLogEntry, error) {
	if occurredAt.IsZero() {
		return SessionLogEntry{}, errors.New("session manifest time is required")
	}
	minimumReaderVersion := manifest.MinReaderVersion
	if minimumReaderVersion == 0 {
		minimumReaderVersion = currentEntryVersion
	}
	payload, err := json.Marshal(SessionLogHeaderPayload{
		TaskSessionID:    manifest.TaskSessionID.String(),
		AgentThreadID:    manifest.AgentThreadID.String(),
		Profile:          string(manifest.Profile),
		WorkspaceKey:     manifest.WorkspaceKey,
		InjectionNonce:   manifest.InjectionNonce,
		MinReaderVersion: minimumReaderVersion,
		WrittenBy:        manifest.WrittenBy,
	})
	if err != nil {
		return SessionLogEntry{}, fmt.Errorf("marshal session manifest: %w", err)
	}
	return SessionLogEntry{
		ID:       "entry-1",
		Sequence: 1,
		At:       occurredAt.UTC(),
		Kind:     SessionLogHeader,
		Version:  currentEntryVersion,
		Payload:  payload,
	}, nil
}

// eventToEntry is the anti-corruption boundary between runtime facts and the
// versioned JSONL schema. The writer, rather than runtime, assigns every
// persistence-only field in the envelope.
func eventToEntry(event agentruntime.SessionEvent, sequence uint64) (SessionLogEntry, error) {
	if sequence == 0 {
		return SessionLogEntry{}, errors.New("session sequence is required")
	}
	if event.OccurredAt.IsZero() {
		return SessionLogEntry{}, errors.New("session event time is required")
	}
	var (
		kind    SessionLogEntryKind
		payload any
	)
	switch event.Kind {
	case agentruntime.SessionEventRunStarted:
		value, ok := event.Payload.(agentruntime.RunStartedEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		toolDefinitionHashes := cloneStringMap(value.Inputs.ToolDefinitionHashes)
		if toolDefinitionHashes == nil {
			toolDefinitionHashes = make(map[string]string)
		}
		kind, payload = SessionLogRunStarted, SessionLogRunStartedPayload{
			Reason:            value.Reason,
			TaskPacketID:      value.Inputs.TaskPacketID.String(),
			ContextManifestID: value.Inputs.ContextManifestID.String(),
			GrantID:           value.Inputs.GrantID.String(),
			Execution: SessionLogExecutionSnapshot{
				SandboxMode:  string(value.Inputs.Execution.SandboxMode),
				ApprovalMode: string(value.Inputs.Execution.ApprovalMode),
			},
			ModelRef:             SessionLogModelRef{ID: value.Inputs.Model.ID},
			SystemPromptHash:     value.Inputs.SystemPromptHash,
			ToolDefHashes:        toolDefinitionHashes,
			ArtifactTemplateHash: value.Inputs.ArtifactTemplateHash,
		}
	case agentruntime.SessionEventMessage:
		value, ok := event.Payload.(agentruntime.MessageEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		message, err := toLogMessage(value.Message)
		if err != nil {
			return SessionLogEntry{}, err
		}
		kind, payload = SessionLogMessage, message
	case agentruntime.SessionEventToolStarted:
		value, ok := event.Payload.(agentruntime.ToolStartedEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		kind, payload = SessionLogToolStarted, SessionLogToolStartedPayload{
			ToolCallID:  value.ToolCallID,
			Name:        value.Name,
			Preflight:   string(value.Preflight),
			BlockReason: value.BlockReason,
		}
	case agentruntime.SessionEventToolSettled:
		value, ok := event.Payload.(agentruntime.ToolSettledEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		kind, payload = SessionLogToolSettled, SessionLogToolSettledPayload{
			ToolCallID: value.ToolCallID,
			Outcome:    value.Outcome,
			ErrorClass: value.ErrorClass,
			DurationMS: value.Duration.Milliseconds(),
			SideEffect: value.SideEffect,
		}
	case agentruntime.SessionEventQueueAdd:
		value, ok := event.Payload.(agentruntime.QueueEnqueuedEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		content, err := toLogContent(value.Content)
		if err != nil {
			return SessionLogEntry{}, err
		}
		kind, payload = SessionLogQueueAdd, SessionLogQueueEnqueuedPayload{
			Queue:   string(value.Queue),
			ItemID:  value.ItemID,
			Content: content,
		}
	case agentruntime.SessionEventQueueTake:
		value, ok := event.Payload.(agentruntime.QueueConsumedEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		kind, payload = SessionLogQueueTake, SessionLogQueueConsumedPayload{
			Queue:  string(value.Queue),
			ItemID: value.ItemID,
			Reason: string(value.Reason),
		}
	case agentruntime.SessionEventRunSettled:
		value, ok := event.Payload.(agentruntime.RunSettledEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		kind, payload = SessionLogRunSettled, SessionLogRunSettledPayload{
			Outcome:    string(value.Outcome),
			ErrorClass: value.ErrorClass,
			TurnCount:  value.TurnCount,
		}
	case agentruntime.SessionEventInterrupted:
		value, ok := event.Payload.(agentruntime.OperationInterruptedEvent)
		if !ok {
			return SessionLogEntry{}, sessionPayloadTypeError(event.Kind, event.Payload)
		}
		kind, payload = SessionLogInterrupted, SessionLogOperationInterruptedPayload{
			Operation: value.Operation,
			TargetID:  value.TargetID,
			Note:      value.Note,
		}
	default:
		return SessionLogEntry{}, fmt.Errorf("unknown runtime session event %q", event.Kind)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return SessionLogEntry{}, fmt.Errorf("marshal session event %q: %w", event.Kind, err)
	}
	return SessionLogEntry{
		ID:       fmt.Sprintf("entry-%d", sequence),
		Sequence: sequence,
		At:       event.OccurredAt.UTC(),
		Kind:     kind,
		Version:  currentEntryVersion,
		RunID:    event.RunID.String(),
		Payload:  encoded,
	}, nil
}

func sessionPayloadTypeError(kind agentruntime.SessionEventKind, payload agentruntime.SessionEventPayload) error {
	return fmt.Errorf("session event %q has incompatible payload %T", kind, payload)
}

func toLogMessage(message agentruntime.TurnMessage) (SessionLogMessagePayload, error) {
	content, err := toLogContent(message.Content)
	if err != nil {
		return SessionLogMessagePayload{}, err
	}
	role := SessionLogMessageRole(message.Role)
	if role != SessionLogRoleUser && role != SessionLogRoleAssistant {
		return SessionLogMessagePayload{}, fmt.Errorf("unknown turn message role %q", message.Role)
	}
	return SessionLogMessagePayload{Role: role, Content: content}, nil
}

func toLogContent(content []agentruntime.TurnContentBlock) ([]SessionLogContentBlock, error) {
	result := make([]SessionLogContentBlock, len(content))
	for index, block := range content {
		mapped, err := toLogContentBlock(block)
		if err != nil {
			return nil, err
		}
		result[index] = mapped
	}
	return result, nil
}

func toLogContentBlock(block agentruntime.TurnContentBlock) (SessionLogContentBlock, error) {
	switch block.Kind {
	case agentruntime.TurnContentText:
		return SessionLogContentBlock{Kind: SessionLogContentText, Text: block.Text}, nil
	case agentruntime.TurnContentThinking:
		return SessionLogContentBlock{
			Kind:      SessionLogContentThinking,
			Text:      block.Text,
			Signature: block.Signature,
		}, nil
	case agentruntime.TurnContentToolUse:
		return SessionLogContentBlock{
			Kind:    SessionLogContentToolUse,
			ToolUse: &SessionLogToolUseBlock{ID: block.ToolCallID, Name: block.ToolName, Input: cloneRaw(block.Input)},
		}, nil
	case agentruntime.TurnContentToolResult:
		return SessionLogContentBlock{
			Kind: SessionLogContentToolResult,
			ToolResult: &SessionLogToolResultBlock{
				ToolCallID: block.ToolCallID,
				Content:    []SessionLogContentBlock{{Kind: SessionLogContentText, Text: block.Text}},
				IsError:    block.IsError,
			},
		}, nil
	default:
		return SessionLogContentBlock{}, fmt.Errorf("unknown turn content kind %q", block.Kind)
	}
}

func fromLogMessage(payload SessionLogMessagePayload) (agentruntime.TurnMessage, bool, error) {
	role := agentruntime.TurnMessageRole(payload.Role)
	if role != agentruntime.TurnRoleUser && role != agentruntime.TurnRoleAssistant {
		return agentruntime.TurnMessage{}, false, fmt.Errorf("unknown persisted message role %q", payload.Role)
	}
	content := make([]agentruntime.TurnContentBlock, 0, len(payload.Content))
	for _, block := range payload.Content {
		mapped, include, err := fromLogContentBlock(block)
		if err != nil {
			return agentruntime.TurnMessage{}, false, err
		}
		if include {
			content = append(content, mapped)
		}
	}
	return agentruntime.TurnMessage{Role: role, Content: content}, len(content) > 0, nil
}

func fromLogContentBlock(block SessionLogContentBlock) (agentruntime.TurnContentBlock, bool, error) {
	switch block.Kind {
	case SessionLogContentText:
		return agentruntime.TurnContentBlock{Kind: agentruntime.TurnContentText, Text: block.Text}, true, nil
	case SessionLogContentThinking:
		return agentruntime.TurnContentBlock{
			Kind:      agentruntime.TurnContentThinking,
			Text:      block.Text,
			Signature: block.Signature,
		}, true, nil
	case SessionLogContentToolUse:
		if block.ToolUse == nil {
			return agentruntime.TurnContentBlock{}, false, errors.New("tool use block is missing its payload")
		}
		return agentruntime.TurnContentBlock{
			Kind:       agentruntime.TurnContentToolUse,
			ToolCallID: block.ToolUse.ID,
			ToolName:   block.ToolUse.Name,
			Input:      cloneRaw(block.ToolUse.Input),
		}, true, nil
	case SessionLogContentToolResult:
		if block.ToolResult == nil {
			return agentruntime.TurnContentBlock{}, false, errors.New("tool result block is missing its payload")
		}
		text, err := logContentText(block.ToolResult.Content)
		if err != nil {
			return agentruntime.TurnContentBlock{}, false, err
		}
		return agentruntime.TurnContentBlock{
			Kind:       agentruntime.TurnContentToolResult,
			ToolCallID: block.ToolResult.ToolCallID,
			Text:       text,
			IsError:    block.ToolResult.IsError,
		}, true, nil
	default:
		return agentruntime.TurnContentBlock{}, false, nil
	}
}

func logContentText(content []SessionLogContentBlock) (string, error) {
	var text strings.Builder
	for _, block := range content {
		switch block.Kind {
		case SessionLogContentText, SessionLogContentThinking:
			text.WriteString(block.Text)
		default:
			return "", fmt.Errorf("tool result contains unsupported content kind %q", block.Kind)
		}
	}
	return text.String(), nil
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
