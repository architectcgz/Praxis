package agentlog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	domainexecution "praxis/internal/domain/execution"
	"strings"
	"time"

	sessionport "praxis/internal/session"
)

const currentEntryVersion uint16 = 2

type entryKind string

const (
	entryHeader           entryKind = "session_header"
	entryExecutionStarted entryKind = "execution_started"
	entryMessage          entryKind = "message"
	entryArtifact         entryKind = "context_artifact"
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
	Profile          string `json:"profile"`
	WorkspaceID      string `json:"workspaceId"`
	InjectionNonce   string `json:"injectionNonce"`
	MinReaderVersion uint16 `json:"minReaderVer"`
	WrittenBy        string `json:"writtenBy"`
}

type executionStartedPayload struct {
	RequestID string                                 `json:"requestId"`
	Reason    domainexecution.ExecutionReason        `json:"reason"`
	Input     domainexecution.ExecutionInputSnapshot `json:"input"`
}

type messagePayload struct {
	MessageID       string                               `json:"messageId"`
	Role            string                               `json:"role"`
	SourceRequestID string                               `json:"sourceRequestId"`
	Content         string                               `json:"content"`
	Blocks          []sessionport.TranscriptContentBlock `json:"blocks,omitempty"`
	PayloadDigest   string                               `json:"payloadDigest"`
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

type artifactPayload struct {
	DeliveryID string          `json:"deliveryId"`
	Kind       string          `json:"kind"`
	ArtifactID string          `json:"artifactId,omitempty"`
	Body       json.RawMessage `json:"body"`
}

type settledPayload struct {
	RequestID   string                               `json:"requestId"`
	Outcome     domainexecution.ExecutionOutcome     `json:"outcome"`
	FailureCode domainexecution.ExecutionFailureCode `json:"failureCode,omitempty"`
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
	if strings.TrimSpace(value.ID) == "" || value.At.IsZero() || value.Version == 0 {
		return errors.New("agent session entry identity, time and version are required")
	}
	if value.Sequence != previous+1 {
		return fmt.Errorf("agent session sequence must advance from %d to %d", previous, previous+1)
	}
	if previous == 0 && value.Kind != entryHeader {
		return errors.New("first agent session entry must be session_header")
	}
	switch value.Kind {
	case entryHeader, entryExecutionStarted, entryMessage, entryArtifact, entryExecutionSettled:
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
			!validMessageRole(message.Role) || len(message.Blocks) == 0 {
			return errors.New("agent session message identity and structured blocks are required")
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

func validateHeader(header sessionport.AgentSessionHeader) error {
	if header.SessionID == "" || header.AgentID == "" || !header.Profile.Valid() ||
		strings.TrimSpace(header.WorkspaceID.String()) == "" || strings.TrimSpace(header.InjectionNonce) == "" ||
		header.MinReaderVersion == 0 || strings.TrimSpace(header.WrittenBy) == "" {
		return errors.New("agent session header is invalid")
	}
	return nil
}

func knownOutcome(outcome domainexecution.ExecutionOutcome) bool {
	switch outcome {
	case domainexecution.ExecutionCompleted, domainexecution.ExecutionYielded, domainexecution.ExecutionPaused,
		domainexecution.ExecutionFailed, domainexecution.ExecutionInterrupted:
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
