package agentlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	"strings"

	coresession "praxis/internal/core/session"
)

func (s *Store) AppendExecutionStart(
	ctx context.Context,
	execution domainexecution.AgentExecution,
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
		for _, value := range entries {
			if value.Kind != entryExecutionStarted || value.ExecutionID != execution.ID.String() {
				continue
			}
			var existing executionStartedPayload
			if err := json.Unmarshal(value.Payload, &existing); err != nil {
				return coresession.ExecutionStartReceipt{}, fmt.Errorf("decode execution start payload: %w", err)
			}
			if existing.RequestID != execution.RequestID.String() || existing.Reason != execution.Reason {
				return coresession.ExecutionStartReceipt{}, domainfoundation.ErrRequestConflict
			}
			break
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
	payload, err := json.Marshal(executionStartedPayload{
		RequestID: execution.RequestID.String(),
		Reason:    execution.Reason,
		Input:     execution.Input,
	})
	if err != nil {
		return coresession.ExecutionStartReceipt{}, fmt.Errorf("encode execution start: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:          s.ids.New("event"),
		At:          s.clock.Now().UTC(),
		Kind:        entryExecutionStarted,
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
	if receipt.ExecutionID == "" || receipt.RequestID == "" || !knownOutcome(receipt.Outcome) {
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
		if existing.RequestID != receipt.RequestID || existing.Outcome != receipt.Outcome || existing.FailureCode != receipt.FailureCode {
			return coresession.ExecutionSettlementReceipt{}, domainfoundation.ErrRequestConflict
		}
		return *existing, nil
	}
	if start, err := startReceipt(entries, receipt.ExecutionID); err != nil {
		return coresession.ExecutionSettlementReceipt{}, err
	} else if start == nil {
		return coresession.ExecutionSettlementReceipt{}, errors.New("execution settlement has no start receipt")
	}
	payload, err := json.Marshal(settledPayload{RequestID: receipt.RequestID.String(), Outcome: receipt.Outcome, FailureCode: receipt.FailureCode})
	if err != nil {
		return coresession.ExecutionSettlementReceipt{}, fmt.Errorf("encode execution settlement: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:          s.ids.New("event"),
		At:          s.clock.Now().UTC(),
		Kind:        entryExecutionSettled,
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
			return coresession.ContextArtifactReceipt{}, domainfoundation.ErrRequestConflict
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
		ID:      s.ids.New("event"),
		At:      s.clock.Now().UTC(),
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

// AppendStructuredMessage records one provider-neutral turn, including tool
// calls and tool results, in the same durable sequence as lifecycle receipts.
func (s *Store) AppendStructuredMessage(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
	messageID string,
	role string,
	sourceRequestID domainfoundation.RequestID,
	blocks []coresession.TranscriptContentBlock,
) error {
	if ctx == nil {
		return errors.New("agent session structured message context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if executionID == "" || strings.TrimSpace(messageID) == "" || (role != "user" && role != "assistant") || len(blocks) == 0 {
		return errors.New("agent session structured message is invalid")
	}
	if sourceRequestID == "" {
		return errors.New("agent session structured message request id is required")
	}
	messageID = strings.TrimSpace(messageID)
	cloned := make([]coresession.TranscriptContentBlock, len(blocks))
	var content strings.Builder
	for i, block := range blocks {
		cloned[i] = block
		cloned[i].Input = append([]byte(nil), block.Input...)
		switch block.Kind {
		case "text":
			content.WriteString(block.Text)
		case "tool_use":
			content.WriteString("[tool:")
			content.WriteString(block.ToolName)
			content.WriteString("]")
		case "tool_result":
			content.WriteString(block.Text)
		default:
			return errors.New("agent session structured message has an unknown block")
		}
	}
	if strings.TrimSpace(content.String()) == "" {
		return errors.New("agent session structured message is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	message := messagePayload{
		MessageID: messageID, Role: role, SourceRequestID: sourceRequestID.String(), Content: content.String(), Blocks: cloned,
	}
	digest, err := digestMessagePayload(message)
	if err != nil {
		return fmt.Errorf("digest agent session structured message: %w", err)
	}
	message.PayloadDigest = digest
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode agent session structured message digest: %w", err)
	}
	entries, err := s.readEntriesLocked()
	if err != nil {
		return err
	}
	if existing, err := messageEntry(entries, executionID, messageID); err != nil {
		return err
	} else if existing != nil {
		if existing.PayloadDigest != message.PayloadDigest {
			return domainfoundation.ErrRequestConflict
		}
		return nil
	}
	if err := s.appendLocked(entry{ID: s.ids.New("event"), At: s.clock.Now().UTC(), Kind: entryMessage,
		Version: currentEntryVersion, ExecutionID: executionID.String(), Payload: payload}); err != nil {
		return err
	}
	return s.syncLocked()
}

func (s *Store) appendMissingInputLocked(execution domainexecution.AgentExecution, entries []entry) error {
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
	message := messagePayload{
		MessageID:       "input:" + execution.RequestID.String(),
		Role:            "user",
		SourceRequestID: execution.RequestID.String(),
		Content:         execution.StartContent,
		Blocks:          []coresession.TranscriptContentBlock{{Kind: "text", Text: execution.StartContent}},
	}
	digest, err := digestMessagePayload(message)
	if err != nil {
		return fmt.Errorf("digest execution input: %w", err)
	}
	message.PayloadDigest = digest
	if existing, err := messageEntry(entries, execution.ID, message.MessageID); err != nil {
		return err
	} else if existing != nil {
		if existing.PayloadDigest != message.PayloadDigest {
			return domainfoundation.ErrRequestConflict
		}
		return nil
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode execution input: %w", err)
	}
	return s.appendLocked(entry{
		ID:          s.ids.New("event"),
		At:          s.clock.Now().UTC(),
		Kind:        entryMessage,
		Version:     currentEntryVersion,
		ExecutionID: execution.ID.String(),
		Payload:     payload,
	})
}
