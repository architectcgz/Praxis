package agentlog

import (
	appcontext "praxis/internal/context"
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	runtimecontract "praxis/internal/runtime"
)

func (s *Store) AppendExecutionStart(
	ctx context.Context,
	execution executionmodel.AgentExecution,
) (runtimecontract.ExecutionStartReceipt, error) {
	if ctx == nil {
		return runtimecontract.ExecutionStartReceipt{}, errors.New("execution start context is required")
	}
	if err := execution.Validate(); err != nil {
		return runtimecontract.ExecutionStartReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return runtimecontract.ExecutionStartReceipt{}, err
	}
	if receipt, err := startReceipt(entries, execution.ID); err != nil || receipt != nil {
		if err != nil {
			return runtimecontract.ExecutionStartReceipt{}, err
		}
		for _, value := range entries {
			if value.Kind != entryExecutionStarted || value.ExecutionID != execution.ID.String() {
				continue
			}
			var existing executionStartedPayload
			if err := json.Unmarshal(value.Payload, &existing); err != nil {
				return runtimecontract.ExecutionStartReceipt{}, fmt.Errorf("decode execution start payload: %w", err)
			}
			if existing.RequestID != execution.RequestID.String() || existing.Reason != execution.Reason {
				return runtimecontract.ExecutionStartReceipt{}, contracts.ErrRequestConflict
			}
			break
		}
		if err := s.appendMissingInputLocked(execution, entries); err != nil {
			return runtimecontract.ExecutionStartReceipt{}, err
		}
		if err := s.syncLocked(); err != nil {
			return runtimecontract.ExecutionStartReceipt{}, err
		}
		entries, err = s.readEntriesLocked()
		if err != nil {
			return runtimecontract.ExecutionStartReceipt{}, err
		}
		receipt, err = startReceipt(entries, execution.ID)
		if err != nil {
			return runtimecontract.ExecutionStartReceipt{}, err
		}
		return *receipt, nil
	}
	if len(entries) == 0 {
		return runtimecontract.ExecutionStartReceipt{}, errors.New(
			"agent session must be initialized before execution start",
		)
	}
	payload, err := json.Marshal(executionStartedPayload{
		RequestID: execution.RequestID.String(),
		Reason:    execution.Reason,
		Input:     execution.Input,
	})
	if err != nil {
		return runtimecontract.ExecutionStartReceipt{}, fmt.Errorf("encode execution start: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:          s.ids.New("event"),
		At:          s.clock.Now().UTC(),
		Kind:        entryExecutionStarted,
		Version:     currentEntryVersion,
		ExecutionID: execution.ID.String(),
		Payload:     payload,
	}); err != nil {
		return runtimecontract.ExecutionStartReceipt{}, err
	}
	if err := s.appendMissingInputLocked(execution, nil); err != nil {
		return runtimecontract.ExecutionStartReceipt{}, err
	}
	if err := s.syncLocked(); err != nil {
		return runtimecontract.ExecutionStartReceipt{}, err
	}
	return s.startReceiptLocked(execution.ID)
}

func (s *Store) AppendExecutionSettlement(
	ctx context.Context,
	receipt runtimecontract.ExecutionSettlementReceipt,
) (runtimecontract.ExecutionSettlementReceipt, error) {
	if ctx == nil {
		return runtimecontract.ExecutionSettlementReceipt{}, errors.New("execution settlement context is required")
	}
	if receipt.ExecutionID == "" || receipt.RequestID == "" || !knownOutcome(receipt.Outcome) {
		return runtimecontract.ExecutionSettlementReceipt{}, errors.New("execution settlement is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return runtimecontract.ExecutionSettlementReceipt{}, err
	}
	if existing, err := settlementReceipt(entries, receipt.ExecutionID); err != nil || existing != nil {
		if err != nil {
			return runtimecontract.ExecutionSettlementReceipt{}, err
		}
		if existing.RequestID != receipt.RequestID || existing.Outcome != receipt.Outcome || existing.FailureCode != receipt.FailureCode {
			return runtimecontract.ExecutionSettlementReceipt{}, contracts.ErrRequestConflict
		}
		return *existing, nil
	}
	if start, err := startReceipt(entries, receipt.ExecutionID); err != nil {
		return runtimecontract.ExecutionSettlementReceipt{}, err
	} else if start == nil {
		return runtimecontract.ExecutionSettlementReceipt{}, errors.New("execution settlement has no start receipt")
	}
	payload, err := json.Marshal(settledPayload{RequestID: receipt.RequestID.String(), Outcome: receipt.Outcome, FailureCode: receipt.FailureCode})
	if err != nil {
		return runtimecontract.ExecutionSettlementReceipt{}, fmt.Errorf("encode execution settlement: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:          s.ids.New("event"),
		At:          s.clock.Now().UTC(),
		Kind:        entryExecutionSettled,
		Version:     currentEntryVersion,
		ExecutionID: receipt.ExecutionID.String(),
		Payload:     payload,
	}); err != nil {
		return runtimecontract.ExecutionSettlementReceipt{}, err
	}
	if err := s.syncLocked(); err != nil {
		return runtimecontract.ExecutionSettlementReceipt{}, err
	}
	return s.settlementReceiptLocked(receipt.ExecutionID)
}

// AppendStructuredMessage records one provider-neutral turn, including tool
// calls and tool results, in the same durable sequence as lifecycle receipts.
func (s *Store) AppendStructuredMessage(
	ctx context.Context,
	executionID contracts.AgentExecutionID,
	messageID string,
	role string,
	sourceRequestID contracts.RequestID,
	blocks []appcontext.ContextBlock,
	thinking ...string,
) error {
	if ctx == nil {
		return errors.New("agent session structured message context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if executionID == "" || strings.TrimSpace(messageID) == "" || !validMessageRole(role) || len(thinking) > 1 ||
		(len(blocks) == 0 && (role != "assistant" || len(thinking) == 0 || strings.TrimSpace(thinking[0]) == "")) ||
		(len(thinking) > 0 && role != "assistant") {
		return errors.New("agent session structured message is invalid")
	}
	if sourceRequestID == "" {
		return errors.New("agent session structured message request id is required")
	}
	messageID = strings.TrimSpace(messageID)
	cloned := make([]appcontext.ContextBlock, len(blocks))
	var content strings.Builder
	for i, block := range blocks {
		cloned[i] = block.Clone()
		switch block.Kind {
		case appcontext.ContextBlockThinking:
			if role != "assistant" {
				return errors.New("thinking blocks require assistant role")
			}
		case appcontext.ContextBlockText:
			content.WriteString(block.Text)
		case appcontext.ContextBlockToolCall:
			content.WriteString("[tool:")
			content.WriteString(block.Name)
			content.WriteString("]")
		case appcontext.ContextBlockToolResult:
			content.WriteString(block.Text)
		default:
			return errors.New("agent session structured message has an unknown block")
		}
	}
	encodedBlocks := transcriptBlocksFromContext(cloned)
	if err := validateTranscriptBlocks(encodedBlocks); err != nil {
		return err
	}
	hasThinkingBlock := false
	for _, block := range blocks {
		if block.Kind == appcontext.ContextBlockThinking {
			hasThinkingBlock = true
			break
		}
	}
	if strings.TrimSpace(content.String()) == "" && len(thinking) == 0 && !hasThinkingBlock {
		return errors.New("agent session structured message is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	message := messagePayload{
		MessageID: messageID, Role: role, SourceRequestID: sourceRequestID.String(), Content: content.String(), Blocks: encodedBlocks,
	}
	if len(thinking) > 0 {
		message.Thinking = thinking[0]
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
			return contracts.ErrRequestConflict
		}
		return nil
	}
	if err := s.appendLocked(entry{ID: s.ids.New("event"), At: s.clock.Now().UTC(), Kind: entryMessage,
		Version: currentEntryVersion, ExecutionID: executionID.String(), Payload: payload}); err != nil {
		return err
	}
	return s.syncLocked()
}

func (s *Store) appendMissingInputLocked(execution executionmodel.AgentExecution, entries []entry) error {
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
		Blocks:          transcriptBlocksFromContext([]appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: execution.StartContent}}),
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
			return contracts.ErrRequestConflict
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
