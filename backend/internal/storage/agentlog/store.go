// Package agentlog implements the target per-Agent append-only JSONL store.
package agentlog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
	"praxis/internal/storage"
)

const currentEntryVersion uint16 = 2

type entryKind string

const (
	entryHeader     entryKind = "session_header"
	entryRunStarted entryKind = "run_started"
	entryMessage    entryKind = "message"
	entryArtifact   entryKind = "context_artifact"
	entryRunSettled entryKind = "run_settled"
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
	WorkspaceKey     string `json:"workspaceKey"`
	InjectionNonce   string `json:"injectionNonce"`
	MinReaderVersion uint16 `json:"minReaderVer"`
	WrittenBy        string `json:"writtenBy"`
}

type runStartedPayload struct {
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

// Store owns one Agent transcript writer. It assigns every sequence number
// and fsyncs receipt boundaries before handing their references to core.
type Store struct {
	path         string
	temporaryDir string

	mu           sync.Mutex
	file         *os.File
	loaded       bool
	lastSequence uint64
}

func Open(root storage.DataRoot, sessionID domain.SessionID, agentID domain.AgentID) (*Store, error) {
	if sessionID == "" || agentID == "" {
		return nil, errors.New("session and agent identifiers are required")
	}
	return NewStore(
		filepath.Join(root.Sessions, sessionID.String(), agentID.String()+".jsonl"),
		root.Temporary,
	)
}

func NewStore(path, temporaryDir string) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return nil, errors.New("agent session path must be an absolute .jsonl path")
	}
	if !filepath.IsAbs(temporaryDir) {
		return nil, errors.New("agent session temporary directory must be absolute")
	}
	return &Store{path: filepath.Clean(path), temporaryDir: filepath.Clean(temporaryDir)}, nil
}

func (s *Store) Initialize(ctx context.Context, header coresession.AgentSessionHeader) error {
	if ctx == nil {
		return errors.New("agent session initialization context is required")
	}
	if err := validateHeader(header); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if s.lastSequence != 0 {
		return s.verifyHeaderLocked(header)
	}
	payload, err := json.Marshal(headerPayload{
		SessionID:        header.SessionID.String(),
		AgentID:          header.AgentID.String(),
		Profile:          string(header.Profile),
		WorkspaceKey:     header.WorkspaceKey,
		InjectionNonce:   header.InjectionNonce,
		MinReaderVersion: header.MinReaderVersion,
		WrittenBy:        header.WrittenBy,
	})
	if err != nil {
		return fmt.Errorf("encode agent session header: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:      domain.NewEventID().String(),
		At:      time.Now().UTC(),
		Kind:    entryHeader,
		Version: currentEntryVersion,
		Payload: payload,
	}); err != nil {
		return err
	}
	return s.syncLocked()
}

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

// ListMessages returns the latest transcript messages in chronological order.
// The read is intentionally separate from AgentSessionStore so runtime code
// cannot use the UI projection to influence execution context or state.
func (s *Store) ListMessages(
	ctx context.Context,
	limit int,
) ([]coresession.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("agent session message context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return nil, err
	}
	messages := make([]coresession.AgentSessionMessage, 0)
	for _, value := range entries {
		if value.Kind != entryMessage {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode transcript message %d: %w", value.Sequence, err)
		}
		messages = append(messages, coresession.AgentSessionMessage{
			Sequence:    value.Sequence,
			At:          value.At,
			ExecutionID: domain.AgentExecutionID(value.ExecutionID),
			Role:        payload.Role,
			Content:     payload.Content,
		})
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
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

// Repair preserves an incomplete final line in the temporary directory and
// refuses to guess through a malformed header or a corrupted complete entry.
func (s *Store) Repair(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, errors.New("agent session repair context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			return false, fmt.Errorf("close agent session before repair: %w", err)
		}
		s.file = nil
	}
	contents, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read agent session for repair: %w", err)
	}
	if _, err := decodeEntries(contents); err == nil {
		return false, nil
	}
	if len(contents) == 0 || contents[len(contents)-1] == '\n' {
		return false, errors.New("agent session has non-tail corruption")
	}
	position := strings.LastIndexByte(string(contents), '\n')
	if position < 0 {
		return false, errors.New("agent session has no complete header")
	}
	complete, tail := contents[:position+1], contents[position+1:]
	if _, err := decodeEntries(complete); err != nil {
		return false, fmt.Errorf("agent session has non-tail corruption: %w", err)
	}
	if err := os.MkdirAll(s.temporaryDir, 0o700); err != nil {
		return false, fmt.Errorf("create agent session repair directory: %w", err)
	}
	backup := filepath.Join(s.temporaryDir, filepath.Base(s.path)+".partial-"+fmt.Sprint(time.Now().UTC().UnixNano()))
	if err := os.WriteFile(backup, tail, 0o600); err != nil {
		return false, fmt.Errorf("backup agent session tail: %w", err)
	}
	if err := os.WriteFile(s.path, complete, 0o600); err != nil {
		return false, fmt.Errorf("truncate agent session tail: %w", err)
	}
	s.loaded = false
	s.lastSequence = 0
	return true, nil
}

func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("agent session close context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	if err := s.syncLocked(); err != nil {
		return err
	}
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close agent session: %w", err)
	}
	s.file = nil
	return nil
}

func (s *Store) entriesLocked() ([]entry, error) {
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return s.readEntriesLocked()
}

func (s *Store) loadLocked() error {
	if s.loaded {
		return nil
	}
	entries, err := s.readEntriesLocked()
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		s.lastSequence = entries[len(entries)-1].Sequence
	}
	s.loaded = true
	return nil
}

func (s *Store) readEntriesLocked() ([]entry, error) {
	if s.file != nil {
		if err := s.syncLocked(); err != nil {
			return nil, err
		}
	}
	contents, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agent session: %w", err)
	}
	return decodeEntries(contents)
}

func (s *Store) appendLocked(value entry) error {
	if err := s.openLocked(); err != nil {
		return err
	}
	value.Sequence = s.lastSequence + 1
	if err := validateEntry(value, s.lastSequence); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode agent session entry: %w", err)
	}
	if _, err := s.file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append agent session entry: %w", err)
	}
	s.lastSequence = value.Sequence
	return nil
}

func (s *Store) openLocked() error {
	if s.file != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create agent session directory: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open agent session: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect agent session: %w", err)
	}
	s.file = file
	return nil
}

func (s *Store) syncLocked() error {
	if s.file == nil {
		return nil
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync agent session: %w", err)
	}
	return nil
}

func (s *Store) verifyHeaderLocked(header coresession.AgentSessionHeader) error {
	entries, err := s.readEntriesLocked()
	if err != nil {
		return err
	}
	if len(entries) == 0 || entries[0].Kind != entryHeader {
		return errors.New("agent session has no header")
	}
	var existing headerPayload
	if err := json.Unmarshal(entries[0].Payload, &existing); err != nil {
		return fmt.Errorf("decode agent session header: %w", err)
	}
	if existing.SessionID != header.SessionID.String() || existing.AgentID != header.AgentID.String() ||
		existing.InjectionNonce != header.InjectionNonce || existing.MinReaderVersion != header.MinReaderVersion {
		return errors.New("agent session header does not match immutable identity")
	}
	return nil
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
	case entryHeader, entryRunStarted, entryMessage, entryArtifact, entryRunSettled:
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
		strings.TrimSpace(header.WorkspaceKey) == "" || strings.TrimSpace(header.InjectionNonce) == "" ||
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

var _ coresession.AgentSessionStore = (*Store)(nil)
