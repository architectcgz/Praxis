package agentlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
)

func (s *Store) AppendExecutionStart(
	ctx context.Context,
	execution domain.AgentExecution,
) (coresession.ExecutionStartReceipt, error) {
	if ctx == nil {
		return coresession.ExecutionStartReceipt{}, errors.New("execution start context is required")
	}
	if err := execution.Validate(); err != nil {
		return coresession.ExecutionStartReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return coresession.ExecutionStartReceipt{}, err
	}
	if receipt, err := startReceipt(entries, execution.ID); err != nil || receipt != nil {
		if err != nil {
			return coresession.ExecutionStartReceipt{}, err
		}
		if err := s.appendMissingInputLocked(execution, entries); err != nil {
			return coresession.ExecutionStartReceipt{}, err
		}
		if err := s.syncLocked(); err != nil {
			return coresession.ExecutionStartReceipt{}, err
		}
		entries, err = s.readEntriesLocked()
		if err != nil {
			return coresession.ExecutionStartReceipt{}, err
		}
		receipt, err = startReceipt(entries, execution.ID)
		if err != nil {
			return coresession.ExecutionStartReceipt{}, err
		}
		return *receipt, nil
	}
	if len(entries) == 0 {
		return coresession.ExecutionStartReceipt{}, errors.New(
			"agent session must be initialized before execution start",
		)
	}
	payload, err := json.Marshal(runStartedPayload{
		RequestID: execution.RequestID.String(),
		Reason:    execution.Reason,
		Input:     execution.Input,
	})
	if err != nil {
		return coresession.ExecutionStartReceipt{}, fmt.Errorf("encode execution start: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:          domain.NewEventID().String(),
		At:          time.Now().UTC(),
		Kind:        entryRunStarted,
		Version:     currentEntryVersion,
		ExecutionID: execution.ID.String(),
		Payload:     payload,
	}); err != nil {
		return coresession.ExecutionStartReceipt{}, err
	}
	if err := s.appendMissingInputLocked(execution, nil); err != nil {
		return coresession.ExecutionStartReceipt{}, err
	}
	if err := s.syncLocked(); err != nil {
		return coresession.ExecutionStartReceipt{}, err
	}
	return s.startReceiptLocked(execution.ID)
}

func (s *Store) AppendExecutionSettlement(
	ctx context.Context,
	receipt coresession.ExecutionSettlementReceipt,
) (coresession.ExecutionSettlementReceipt, error) {
	if ctx == nil {
		return coresession.ExecutionSettlementReceipt{}, errors.New("execution settlement context is required")
	}
	if receipt.ExecutionID == "" || !knownOutcome(receipt.Outcome) {
		return coresession.ExecutionSettlementReceipt{}, errors.New("execution settlement is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return coresession.ExecutionSettlementReceipt{}, err
	}
	if existing, err := settlementReceipt(entries, receipt.ExecutionID); err != nil || existing != nil {
		if err != nil {
			return coresession.ExecutionSettlementReceipt{}, err
		}
		return *existing, nil
	}
	if start, err := startReceipt(entries, receipt.ExecutionID); err != nil {
		return coresession.ExecutionSettlementReceipt{}, err
	} else if start == nil {
		return coresession.ExecutionSettlementReceipt{}, errors.New("execution settlement has no start receipt")
	}
	payload, err := json.Marshal(settledPayload{Outcome: receipt.Outcome, FailureCode: receipt.FailureCode})
	if err != nil {
		return coresession.ExecutionSettlementReceipt{}, fmt.Errorf("encode execution settlement: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:          domain.NewEventID().String(),
		At:          time.Now().UTC(),
		Kind:        entryRunSettled,
		Version:     currentEntryVersion,
		ExecutionID: receipt.ExecutionID.String(),
		Payload:     payload,
	}); err != nil {
		return coresession.ExecutionSettlementReceipt{}, err
	}
	if err := s.syncLocked(); err != nil {
		return coresession.ExecutionSettlementReceipt{}, err
	}
	return s.settlementReceiptLocked(receipt.ExecutionID)
}

func (s *Store) AppendContextArtifact(
	ctx context.Context,
	artifact coresession.ContextArtifact,
) (coresession.ContextArtifactReceipt, error) {
	if ctx == nil {
		return coresession.ContextArtifactReceipt{}, errors.New("context artifact context is required")
	}
	if artifact.DeliveryID == "" || strings.TrimSpace(artifact.Kind) == "" || len(artifact.Body) == 0 {
		return coresession.ContextArtifactReceipt{}, errors.New("context artifact is invalid")
	}
	var body any
	if err := json.Unmarshal(artifact.Body, &body); err != nil {
		return coresession.ContextArtifactReceipt{}, fmt.Errorf("context artifact body is invalid JSON: %w", err)
	}
	if containsSensitiveValue(body) {
		return coresession.ContextArtifactReceipt{}, errors.New("context artifact body contains a sensitive field")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return coresession.ContextArtifactReceipt{}, err
	}
	existingArtifact, existing, err := artifactEntry(entries, artifact.DeliveryID)
	if err != nil {
		return coresession.ContextArtifactReceipt{}, err
	}
	if existing != nil {
		if existingArtifact.Kind != strings.TrimSpace(artifact.Kind) ||
			existingArtifact.ArtifactID != strings.TrimSpace(artifact.ArtifactID) ||
			!bytes.Equal(existingArtifact.Body, artifact.Body) {
			return coresession.ContextArtifactReceipt{}, domain.ErrRequestConflict
		}
		return *existing, nil
	}
	payload, err := json.Marshal(artifactPayload{
		DeliveryID: artifact.DeliveryID.String(),
		Kind:       strings.TrimSpace(artifact.Kind),
		ArtifactID: strings.TrimSpace(artifact.ArtifactID),
		Body:       append(json.RawMessage(nil), artifact.Body...),
	})
	if err != nil {
		return coresession.ContextArtifactReceipt{}, fmt.Errorf("encode context artifact: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:      domain.NewEventID().String(),
		At:      time.Now().UTC(),
		Kind:    entryArtifact,
		Version: currentEntryVersion,
		Payload: payload,
	}); err != nil {
		return coresession.ContextArtifactReceipt{}, err
	}
	if err := s.syncLocked(); err != nil {
		return coresession.ContextArtifactReceipt{}, err
	}
	return s.artifactReceiptLocked(artifact.DeliveryID)
}

// AppendMessage records a provider-neutral transcript message at a durable
// execution boundary. Runtime adapters use this instead of writing JSONL.
func (s *Store) AppendMessage(
	ctx context.Context,
	executionID domain.AgentExecutionID,
	role string,
	sourceRequestID domain.RequestID,
	content string,
) error {
	if ctx == nil {
		return errors.New("agent session message context is required")
	}
	if executionID == "" || (role != "user" && role != "assistant") || strings.TrimSpace(content) == "" {
		return errors.New("agent session message is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	payload, err := json.Marshal(messagePayload{
		Role: role, SourceRequestID: sourceRequestID.String(), Content: content,
	})
	if err != nil {
		return fmt.Errorf("encode agent session message: %w", err)
	}
	if err := s.appendLocked(entry{ID: domain.NewEventID().String(), At: time.Now().UTC(), Kind: entryMessage,
		Version: currentEntryVersion, ExecutionID: executionID.String(), Payload: payload}); err != nil {
		return err
	}
	return s.syncLocked()
}

func (s *Store) appendMissingInputLocked(execution domain.AgentExecution, entries []entry) error {
	if execution.StartContent == "" {
		return nil
	}
	if entries == nil {
		var err error
		entries, err = s.readEntriesLocked()
		if err != nil {
			return err
		}
	}
	if _, found, err := executionInputDigest(entries, execution.ID, execution.RequestID); err != nil {
		return err
	} else if found {
		return nil
	}
	payload, err := json.Marshal(messagePayload{
		Role:            "user",
		SourceRequestID: execution.RequestID.String(),
		Content:         execution.StartContent,
	})
	if err != nil {
		return fmt.Errorf("encode execution input: %w", err)
	}
	return s.appendLocked(entry{
		ID:          domain.NewEventID().String(),
		At:          time.Now().UTC(),
		Kind:        entryMessage,
		Version:     currentEntryVersion,
		ExecutionID: execution.ID.String(),
		Payload:     payload,
	})
}
