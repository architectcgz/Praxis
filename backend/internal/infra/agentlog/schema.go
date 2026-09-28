package agentlog

import (
	appcontext "praxis/internal/context"
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	runtimecontract "praxis/internal/runtime"
)

const currentEntryVersion uint16 = 5

type entryKind string

const (
	entryHeader           entryKind = "session_header"
	entryExecutionStarted entryKind = "execution_started"
	entryMessage          entryKind = "message"
	entryExecutionSettled entryKind = "execution_settled"
)

type entry struct {
	ID          string          `json:"id"`
	Sequence    uint64          `json:"seq"`
	At          time.Time       `json:"at"`
	Kind        entryKind       `json:"kind"`
	Version     uint16          `json:"ver"`
	ExecutionID string          `json:"executionId,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

type headerPayload struct {
	SessionID        string `json:"sessionId"`
	AgentID          string `json:"agentId"`
	DefinitionID     string `json:"definitionId"`
	Profile          string `json:"profile"`
	WorkspaceID      string `json:"workspaceId"`
	InjectionNonce   string `json:"injectionNonce"`
	MinReaderVersion uint16 `json:"minReaderVer"`
	WrittenBy        string `json:"writtenBy"`
}

type executionStartedPayload struct {
	RequestID string                                `json:"requestId"`
	Reason    executionmodel.ExecutionReason        `json:"reason"`
	Input     executionmodel.ExecutionInputSnapshot `json:"input"`
}

type messagePayload struct {
	MessageID       string            `json:"messageId"`
	Role            string            `json:"role"`
	SourceRequestID string            `json:"sourceRequestId"`
	Content         string            `json:"content"`
	Thinking        string            `json:"thinking,omitempty"`
	Blocks          []transcriptBlock `json:"blocks"`
	PayloadDigest   string            `json:"payloadDigest"`
}

type transcriptBlock struct {
	Kind    appcontext.ContextBlockKind `json:"kind"`
	Text    string                      `json:"text,omitempty"`
	CallID  string                      `json:"callId,omitempty"`
	Name    string                      `json:"name,omitempty"`
	Input   json.RawMessage             `json:"input,omitempty"`
	IsError bool                        `json:"isError,omitempty"`
}

func digestMessagePayload(payload messagePayload) (string, error) {
	payload.PayloadDigest = ""
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type settledPayload struct {
	RequestID   string                          `json:"requestId"`
	Outcome     executionmodel.ExecutionOutcome `json:"outcome"`
	FailureCode contracts.ExecutionFailureCode  `json:"failureCode,omitempty"`
}

func decodeEntries(contents []byte) ([]entry, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	if contents[len(contents)-1] != '\n' {
		return nil, errors.New("agent session has an incomplete tail")
	}
	lines := strings.Split(string(contents[:len(contents)-1]), "\n")
	entries := make([]entry, 0, len(lines))
	var previous uint64
	for index, line := range lines {
		if line == "" {
			return nil, fmt.Errorf("agent session line %d is empty", index+1)
		}
		var value entry
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return nil, fmt.Errorf("agent session line %d is invalid: %w", index+1, err)
		}
		if err := validateEntry(value, previous); err != nil {
			return nil, fmt.Errorf("agent session line %d: %w", index+1, err)
		}
		previous = value.Sequence
		entries = append(entries, value)
	}
	return entries, nil
}

func validateEntry(value entry, previous uint64) error {
	if strings.TrimSpace(value.ID) == "" || value.At.IsZero() || value.Version != currentEntryVersion {
		return errors.New("agent session entry identity, time and version are required")
	}
	if value.Sequence != previous+1 {
		return fmt.Errorf("agent session sequence must advance from %d to %d", previous, previous+1)
	}
	if previous == 0 && value.Kind != entryHeader {
		return errors.New("first agent session entry must be session_header")
	}
	switch value.Kind {
	case entryHeader, entryExecutionStarted, entryMessage, entryExecutionSettled:
	default:
		return fmt.Errorf("unknown agent session entry kind %q", value.Kind)
	}
	var payload any
	if err := json.Unmarshal(value.Payload, &payload); err != nil {
		return fmt.Errorf("agent session payload is invalid JSON: %w", err)
	}
	if containsSensitiveValue(payload) {
		return errors.New("agent session payload contains a sensitive field")
	}
	if value.Kind == entryMessage {
		var message messagePayload
		if err := json.Unmarshal(value.Payload, &message); err != nil {
			return fmt.Errorf("agent session message payload is invalid: %w", err)
		}
		if strings.TrimSpace(message.MessageID) == "" || strings.TrimSpace(message.PayloadDigest) == "" ||
			!validMessageRole(message.Role) || (len(message.Blocks) == 0 && (message.Role != "assistant" || strings.TrimSpace(message.Thinking) == "")) ||
			(message.Thinking != "" && message.Role != "assistant") {
			return errors.New("agent session message identity and structured blocks are required")
		}
		if err := validateTranscriptBlocks(message.Blocks); err != nil {
			return err
		}
		if message.Role != "assistant" {
			for _, block := range message.Blocks {
				if block.Kind == appcontext.ContextBlockThinking {
					return errors.New("thinking blocks require assistant role")
				}
			}
		}
		digest, err := digestMessagePayload(message)
		if err != nil {
			return fmt.Errorf("digest agent session message payload: %w", err)
		}
		if digest != message.PayloadDigest {
			return errors.New("agent session message payload digest does not match")
		}
	}
	return nil
}

func validMessageRole(role string) bool {
	return role == "user" || role == "assistant" || role == "tool"
}

func validateHeader(header runtimecontract.AgentSessionHeader) error {
	if header.SessionID == "" || header.AgentID == "" || !header.DefinitionID.Valid() || !header.Profile.Valid() ||
		strings.TrimSpace(header.WorkspaceID.String()) == "" || strings.TrimSpace(header.InjectionNonce) == "" ||
		header.MinReaderVersion == 0 || strings.TrimSpace(header.WrittenBy) == "" {
		return errors.New("agent session header is invalid")
	}
	return nil
}

func knownOutcome(outcome executionmodel.ExecutionOutcome) bool {
	switch outcome {
	case executionmodel.ExecutionCompleted, executionmodel.ExecutionYielded, executionmodel.ExecutionPaused,
		executionmodel.ExecutionFailed, executionmodel.ExecutionInterrupted:
		return true
	default:
		return false
	}
}

func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "apikey", "api_key", "authorization", "secret", "token", "providerpayload",
				"provider_payload", "hiddenprompt", "hidden_prompt":
				return true
			}
			if containsSensitiveValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveValue(child) {
				return true
			}
		}
	}
	return false
}

func validateTranscriptBlocks(blocks []transcriptBlock) error {
	for index, block := range blocks {
		switch block.Kind {
		case appcontext.ContextBlockThinking:
			if strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
				return fmt.Errorf("transcript block %d has invalid thinking fields", index)
			}
		case appcontext.ContextBlockText:
			if strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
				return fmt.Errorf("transcript block %d has invalid text fields", index)
			}
		case appcontext.ContextBlockToolCall:
			if strings.TrimSpace(block.CallID) == "" || strings.TrimSpace(block.Name) == "" || block.IsError ||
				(len(block.Input) > 0 && !json.Valid(block.Input)) {
				return fmt.Errorf("transcript block %d has invalid tool call fields", index)
			}
		case appcontext.ContextBlockToolResult:
			if strings.TrimSpace(block.CallID) == "" || strings.TrimSpace(block.Name) == "" {
				return fmt.Errorf("transcript block %d has invalid tool result fields", index)
			}
		default:
			return fmt.Errorf("transcript block %d has unknown kind %q", index, block.Kind)
		}
	}
	return nil
}

func transcriptBlocksFromContext(blocks []appcontext.ContextBlock) []transcriptBlock {
	result := make([]transcriptBlock, len(blocks))
	for index, block := range blocks {
		result[index] = transcriptBlock{
			Kind:    block.Kind,
			Text:    block.Text,
			CallID:  block.CallID,
			Name:    block.Name,
			Input:   append(json.RawMessage(nil), block.Input...),
			IsError: block.IsError,
		}
	}
	return result
}

func contextBlocksFromTranscript(blocks []transcriptBlock) []appcontext.ContextBlock {
	result := make([]appcontext.ContextBlock, len(blocks))
	for index, block := range blocks {
		result[index] = appcontext.ContextBlock{
			Kind:    block.Kind,
			Text:    block.Text,
			CallID:  block.CallID,
			Name:    block.Name,
			Input:   append(json.RawMessage(nil), block.Input...),
			IsError: block.IsError,
		}
	}
	return result
}
