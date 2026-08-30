package agentlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
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
	RequestID string                        `json:"requestId"`
	Reason    domain.ExecutionReason        `json:"reason"`
	Input     domain.ExecutionInputSnapshot `json:"input"`
}

type messagePayload struct {
	Role            string `json:"role"`
	SourceRequestID string `json:"sourceRequestId"`
	Content         string `json:"content"`
}

type artifactPayload struct {
	DeliveryID string          `json:"deliveryId"`
	Kind       string          `json:"kind"`
	ArtifactID string          `json:"artifactId,omitempty"`
	Body       json.RawMessage `json:"body"`
}

type settledPayload struct {
	Outcome     domain.ExecutionOutcome     `json:"outcome"`
	FailureCode domain.ExecutionFailureCode `json:"failureCode,omitempty"`
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
	return nil
}

func validateHeader(header coresession.AgentSessionHeader) error {
	if header.SessionID == "" || header.AgentID == "" || !header.Profile.Valid() ||
		strings.TrimSpace(header.WorkspaceID.String()) == "" || strings.TrimSpace(header.InjectionNonce) == "" ||
		header.MinReaderVersion == 0 || strings.TrimSpace(header.WrittenBy) == "" {
		return errors.New("agent session header is invalid")
	}
	return nil
}

func knownOutcome(outcome domain.ExecutionOutcome) bool {
	switch outcome {
	case domain.ExecutionCompleted, domain.ExecutionYielded, domain.ExecutionPaused,
		domain.ExecutionFailed, domain.ExecutionInterrupted:
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
