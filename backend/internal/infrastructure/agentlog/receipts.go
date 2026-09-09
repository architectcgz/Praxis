package agentlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	domainfoundation "praxis/internal/domain/foundation"

	sessionport "praxis/internal/session"
)

func (s *Store) FindExecutionStart(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
) (*sessionport.ExecutionStartReceipt, error) {
	if ctx == nil {
		return nil, errors.New("execution start lookup context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return nil, err
	}
	return startReceipt(entries, executionID)
}

func (s *Store) FindExecutionSettlement(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
) (*sessionport.ExecutionSettlementReceipt, error) {
	if ctx == nil {
		return nil, errors.New("execution settlement lookup context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return nil, err
	}
	return settlementReceipt(entries, executionID)
}

func (s *Store) FindContextArtifact(
	ctx context.Context,
	deliveryID domainfoundation.DeliveryID,
) (*sessionport.ContextArtifactReceipt, error) {
	if ctx == nil {
		return nil, errors.New("context artifact lookup context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return nil, err
	}
	return artifactReceipt(entries, deliveryID)
}

func (s *Store) startReceiptLocked(executionID domainfoundation.AgentExecutionID) (sessionport.ExecutionStartReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return sessionport.ExecutionStartReceipt{}, err
	}
	receipt, err := startReceipt(entries, executionID)
	if err != nil || receipt == nil {
		if err != nil {
			return sessionport.ExecutionStartReceipt{}, err
		}
		return sessionport.ExecutionStartReceipt{}, errors.New("execution start receipt was not written")
	}
	return *receipt, nil
}

func (s *Store) settlementReceiptLocked(
	executionID domainfoundation.AgentExecutionID,
) (sessionport.ExecutionSettlementReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return sessionport.ExecutionSettlementReceipt{}, err
	}
	receipt, err := settlementReceipt(entries, executionID)
	if err != nil || receipt == nil {
		if err != nil {
			return sessionport.ExecutionSettlementReceipt{}, err
		}
		return sessionport.ExecutionSettlementReceipt{}, errors.New("execution settlement receipt was not written")
	}
	return *receipt, nil
}

func (s *Store) artifactReceiptLocked(
	deliveryID domainfoundation.DeliveryID,
) (sessionport.ContextArtifactReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return sessionport.ContextArtifactReceipt{}, err
	}
	receipt, err := artifactReceipt(entries, deliveryID)
	if err != nil || receipt == nil {
		if err != nil {
			return sessionport.ContextArtifactReceipt{}, err
		}
		return sessionport.ContextArtifactReceipt{}, errors.New("context artifact receipt was not written")
	}
	return *receipt, nil
}

func startReceipt(entries []entry, executionID domainfoundation.AgentExecutionID) (*sessionport.ExecutionStartReceipt, error) {
	for _, value := range entries {
		if value.Kind != entryExecutionStarted || value.ExecutionID != executionID.String() {
			continue
		}
		var payload executionStartedPayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode execution start receipt: %w", err)
		}
		digest, _, err := executionInputDigest(entries, executionID, domainfoundation.RequestID(payload.RequestID))
		if err != nil {
			return nil, err
		}
		return &sessionport.ExecutionStartReceipt{
			ExecutionID: executionID,
			RequestID:   domainfoundation.RequestID(payload.RequestID),
			EntryID:     value.ID,
			Sequence:    value.Sequence,
			InputDigest: digest,
		}, nil
	}
	return nil, nil
}

func executionInputDigest(
	entries []entry,
	executionID domainfoundation.AgentExecutionID,
	requestID domainfoundation.RequestID,
) (string, bool, error) {
	for _, value := range entries {
		if value.Kind != entryMessage || value.ExecutionID != executionID.String() {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return "", false, fmt.Errorf("decode execution input receipt: %w", err)
		}
		if payload.Role != "user" || payload.MessageID != "input:"+requestID.String() || payload.SourceRequestID != requestID.String() {
			continue
		}
		digest := sha256.Sum256([]byte(payload.Content))
		return hex.EncodeToString(digest[:]), true, nil
	}
	return "", false, nil
}

func settlementReceipt(
	entries []entry,
	executionID domainfoundation.AgentExecutionID,
) (*sessionport.ExecutionSettlementReceipt, error) {
	for _, value := range entries {
		if value.Kind != entryExecutionSettled || value.ExecutionID != executionID.String() {
			continue
		}
		var payload settledPayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode execution settlement receipt: %w", err)
		}
		if payload.RequestID == "" || !knownOutcome(payload.Outcome) {
			return nil, errors.New("execution settlement receipt is missing request identity or outcome")
		}
		return &sessionport.ExecutionSettlementReceipt{
			ExecutionID: executionID, RequestID: domainfoundation.RequestID(payload.RequestID),
			EntryID:     value.ID,
			Sequence:    value.Sequence,
			Outcome:     payload.Outcome,
			FailureCode: payload.FailureCode,
		}, nil
	}
	return nil, nil
}

func artifactReceipt(entries []entry, deliveryID domainfoundation.DeliveryID) (*sessionport.ContextArtifactReceipt, error) {
	_, receipt, err := artifactEntry(entries, deliveryID)
	return receipt, err
}

func artifactEntry(
	entries []entry,
	deliveryID domainfoundation.DeliveryID,
) (*artifactPayload, *sessionport.ContextArtifactReceipt, error) {
	for _, value := range entries {
		if value.Kind != entryArtifact {
			continue
		}
		var payload artifactPayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("decode context artifact receipt: %w", err)
		}
		if payload.DeliveryID == deliveryID.String() {
			receipt := &sessionport.ContextArtifactReceipt{
				DeliveryID: deliveryID,
				EntryID:    value.ID,
				Sequence:   value.Sequence,
			}
			return &payload, receipt, nil
		}
	}
	return nil, nil, nil
}

func messageEntry(
	entries []entry,
	executionID domainfoundation.AgentExecutionID,
	messageID string,
) (*messagePayload, error) {
	for _, value := range entries {
		if value.Kind != entryMessage || value.ExecutionID != executionID.String() {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode transcript message: %w", err)
		}
		if payload.MessageID == messageID {
			return &payload, nil
		}
	}
	return nil, nil
}

var _ sessionport.TranscriptReceiptStore = (*Store)(nil)
var _ sessionport.TranscriptMessageStore = (*Store)(nil)
