package agentlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
)

func (s *Store) FindExecutionStart(
	ctx context.Context,
	executionID domain.AgentExecutionID,
) (*coresession.ExecutionStartReceipt, error) {
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
	executionID domain.AgentExecutionID,
) (*coresession.ExecutionSettlementReceipt, error) {
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
	deliveryID domain.DeliveryID,
) (*coresession.ContextArtifactReceipt, error) {
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

func (s *Store) startReceiptLocked(executionID domain.AgentExecutionID) (coresession.ExecutionStartReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return coresession.ExecutionStartReceipt{}, err
	}
	receipt, err := startReceipt(entries, executionID)
	if err != nil || receipt == nil {
		if err != nil {
			return coresession.ExecutionStartReceipt{}, err
		}
		return coresession.ExecutionStartReceipt{}, errors.New("execution start receipt was not written")
	}
	return *receipt, nil
}

func (s *Store) settlementReceiptLocked(
	executionID domain.AgentExecutionID,
) (coresession.ExecutionSettlementReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return coresession.ExecutionSettlementReceipt{}, err
	}
	receipt, err := settlementReceipt(entries, executionID)
	if err != nil || receipt == nil {
		if err != nil {
			return coresession.ExecutionSettlementReceipt{}, err
		}
		return coresession.ExecutionSettlementReceipt{}, errors.New("execution settlement receipt was not written")
	}
	return *receipt, nil
}

func (s *Store) artifactReceiptLocked(
	deliveryID domain.DeliveryID,
) (coresession.ContextArtifactReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return coresession.ContextArtifactReceipt{}, err
	}
	receipt, err := artifactReceipt(entries, deliveryID)
	if err != nil || receipt == nil {
		if err != nil {
			return coresession.ContextArtifactReceipt{}, err
		}
		return coresession.ContextArtifactReceipt{}, errors.New("context artifact receipt was not written")
	}
	return *receipt, nil
}

func startReceipt(entries []entry, executionID domain.AgentExecutionID) (*coresession.ExecutionStartReceipt, error) {
	for _, value := range entries {
		if value.Kind != entryRunStarted || value.ExecutionID != executionID.String() {
			continue
		}
		var payload runStartedPayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode execution start receipt: %w", err)
		}
		digest, _, err := executionInputDigest(entries, executionID, domain.RequestID(payload.RequestID))
		if err != nil {
			return nil, err
		}
		return &coresession.ExecutionStartReceipt{
			ExecutionID: executionID,
			RequestID:   domain.RequestID(payload.RequestID),
			EntryID:     value.ID,
			Sequence:    value.Sequence,
			InputDigest: digest,
		}, nil
	}
	return nil, nil
}

func executionInputDigest(
	entries []entry,
	executionID domain.AgentExecutionID,
	requestID domain.RequestID,
) (string, bool, error) {
	for _, value := range entries {
		if value.Kind != entryMessage || value.ExecutionID != executionID.String() {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return "", false, fmt.Errorf("decode execution input receipt: %w", err)
		}
		if payload.SourceRequestID != requestID.String() {
			continue
		}
		digest := sha256.Sum256([]byte(payload.Content))
		return hex.EncodeToString(digest[:]), true, nil
	}
	return "", false, nil
}

func settlementReceipt(
	entries []entry,
	executionID domain.AgentExecutionID,
) (*coresession.ExecutionSettlementReceipt, error) {
	for _, value := range entries {
		if value.Kind != entryRunSettled || value.ExecutionID != executionID.String() {
			continue
		}
		var payload settledPayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode execution settlement receipt: %w", err)
		}
		return &coresession.ExecutionSettlementReceipt{
			ExecutionID: executionID,
			EntryID:     value.ID,
			Sequence:    value.Sequence,
			Outcome:     payload.Outcome,
			FailureCode: payload.FailureCode,
		}, nil
	}
	return nil, nil
}

func artifactReceipt(entries []entry, deliveryID domain.DeliveryID) (*coresession.ContextArtifactReceipt, error) {
	_, receipt, err := artifactEntry(entries, deliveryID)
	return receipt, err
}

func artifactEntry(
	entries []entry,
	deliveryID domain.DeliveryID,
) (*artifactPayload, *coresession.ContextArtifactReceipt, error) {
	for _, value := range entries {
		if value.Kind != entryArtifact {
			continue
		}
		var payload artifactPayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("decode context artifact receipt: %w", err)
		}
		if payload.DeliveryID == deliveryID.String() {
			receipt := &coresession.ContextArtifactReceipt{
				DeliveryID: deliveryID,
				EntryID:    value.ID,
				Sequence:   value.Sequence,
			}
			return &payload, receipt, nil
		}
	}
	return nil, nil, nil
}

var _ coresession.TranscriptReceiptStore = (*Store)(nil)
