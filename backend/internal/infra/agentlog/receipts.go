package agentlog

import (
	"praxis/internal/contracts"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	runtimecontract "praxis/internal/runtime"
)

func (s *Store) startReceiptLocked(executionID contracts.AgentExecutionID) (runtimecontract.ExecutionStartReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return runtimecontract.ExecutionStartReceipt{}, err
	}
	receipt, err := startReceipt(entries, executionID)
	if err != nil || receipt == nil {
		if err != nil {
			return runtimecontract.ExecutionStartReceipt{}, err
		}
		return runtimecontract.ExecutionStartReceipt{}, errors.New("execution start receipt was not written")
	}
	return *receipt, nil
}

func (s *Store) settlementReceiptLocked(
	executionID contracts.AgentExecutionID,
) (runtimecontract.ExecutionSettlementReceipt, error) {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return runtimecontract.ExecutionSettlementReceipt{}, err
	}
	receipt, err := settlementReceipt(entries, executionID)
	if err != nil || receipt == nil {
		if err != nil {
			return runtimecontract.ExecutionSettlementReceipt{}, err
		}
		return runtimecontract.ExecutionSettlementReceipt{}, errors.New("execution settlement receipt was not written")
	}
	return *receipt, nil
}

func startReceipt(entries []entry, executionID contracts.AgentExecutionID) (*runtimecontract.ExecutionStartReceipt, error) {
	for _, value := range entries {
		if value.Kind != entryExecutionStarted || value.ExecutionID != executionID.String() {
			continue
		}
		var payload executionStartedPayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode execution start receipt: %w", err)
		}
		digest, _, err := executionInputDigest(entries, executionID, contracts.RequestID(payload.RequestID))
		if err != nil {
			return nil, err
		}
		return &runtimecontract.ExecutionStartReceipt{
			ExecutionID: executionID,
			RequestID:   contracts.RequestID(payload.RequestID),
			EntryID:     value.ID,
			Sequence:    value.Sequence,
			InputDigest: digest,
		}, nil
	}
	return nil, nil
}

func executionInputDigest(
	entries []entry,
	executionID contracts.AgentExecutionID,
	requestID contracts.RequestID,
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
	executionID contracts.AgentExecutionID,
) (*runtimecontract.ExecutionSettlementReceipt, error) {
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
		return &runtimecontract.ExecutionSettlementReceipt{
			ExecutionID: executionID, RequestID: contracts.RequestID(payload.RequestID),
			EntryID:     value.ID,
			Sequence:    value.Sequence,
			Outcome:     payload.Outcome,
			FailureCode: payload.FailureCode,
		}, nil
	}
	return nil, nil
}

func messageEntry(
	entries []entry,
	executionID contracts.AgentExecutionID,
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

var _ runtimecontract.TranscriptReceiptStore = (*Store)(nil)
var _ runtimecontract.TranscriptMessageStore = (*Store)(nil)
var _ runtimecontract.TranscriptStore = (*Store)(nil)
