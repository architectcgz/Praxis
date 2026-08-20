package sessionlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"praxis/internal/agentruntime"
	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
	"praxis/internal/storage"
)

// Store owns the single append writer for one AgentThread JSONL transcript.
// It translates runtime events into the versioned sessionlog schema and never
// exposes raw durable records to runtime callers.
type Store struct {
	path         string
	temporaryDir string
	mu           sync.Mutex
	file         *os.File
	lastSequence uint64
	loaded       bool
}

// Open creates a per-thread store using the fixed DataRoot session layout.
func Open(root storage.DataRoot, sessionID domain.TaskSessionID, threadID domain.AgentThreadID) (*Store, error) {
	if sessionID == "" || threadID == "" {
		return nil, errors.New("session and thread identifiers are required")
	}
	path := filepath.Join(root.Sessions, sessionID.String(), threadID.String()+".jsonl")
	return NewStore(path, root.Temporary)
}

// NewStore creates a store for an absolute JSONL path without creating an entry.
func NewStore(path, temporaryDir string) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return nil, errors.New("session log path must be an absolute .jsonl path")
	}
	if !filepath.IsAbs(temporaryDir) {
		return nil, errors.New("session log temporary directory must be absolute")
	}
	return &Store{path: filepath.Clean(path), temporaryDir: filepath.Clean(temporaryDir)}, nil
}

// Path returns the immutable location used by this store.
func (s *Store) Path() string { return s.path }

// Initialize writes the immutable manifest as the first JSONL entry for a session.
func (s *Store) Initialize(
	ctx context.Context,
	manifest agentruntime.SessionManifest,
	occurredAt time.Time,
) (agentruntime.SessionAppendResult, error) {
	if ctx == nil {
		return agentruntime.SessionAppendResult{}, errors.New("session initialization context is required")
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	if s.lastSequence != 0 {
		return agentruntime.SessionAppendResult{}, errors.New("session log is already initialized")
	}
	entry, err := manifestToEntry(manifest, occurredAt)
	if err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	if err := validateEntry(entry, 0); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	if err := s.openForAppendLocked(); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return agentruntime.SessionAppendResult{}, fmt.Errorf("marshal session manifest: %w", err)
	}
	if _, err := s.file.Write(append(encoded, '\n')); err != nil {
		return agentruntime.SessionAppendResult{}, fmt.Errorf("append session manifest: %w", err)
	}
	s.lastSequence = entry.Sequence
	return agentruntime.SessionAppendResult{EntryID: entry.ID, Sequence: entry.Sequence}, nil
}

// Append converts one runtime event to a durable JSONL entry. The writer owns
// sequence assignment so concurrent runtime producers cannot create gaps or duplicates.
func (s *Store) Append(ctx context.Context, event agentruntime.SessionEvent) (agentruntime.SessionAppendResult, error) {
	if ctx == nil {
		return agentruntime.SessionAppendResult{}, errors.New("session append context is required")
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	if s.lastSequence == 0 {
		return agentruntime.SessionAppendResult{}, errors.New("session log must be initialized before appending events")
	}
	sequence := s.lastSequence + 1
	entry, err := eventToEntry(event, sequence)
	if err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	if err := validateEntry(entry, s.lastSequence); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	if err := s.openForAppendLocked(); err != nil {
		return agentruntime.SessionAppendResult{}, err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return agentruntime.SessionAppendResult{}, fmt.Errorf("marshal session entry: %w", err)
	}
	if _, err := s.file.Write(append(encoded, '\n')); err != nil {
		return agentruntime.SessionAppendResult{}, fmt.Errorf("append session entry: %w", err)
	}
	s.lastSequence = sequence
	return agentruntime.SessionAppendResult{EntryID: entry.ID, Sequence: sequence}, nil
}

// Flush establishes the runtime save-point durability boundary.
func (s *Store) Flush(ctx context.Context) error {
	if ctx == nil {
		return errors.New("session flush context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync session log: %w", err)
	}
	return nil
}

// ReadContext returns the safe provider-facing projection; audit-only records
// remain inside this package and are never exposed as runtime types.
func (s *Store) ReadContext(ctx context.Context, _ string) (agentruntime.SessionContext, error) {
	if ctx == nil {
		return agentruntime.SessionContext{}, errors.New("session read context is required")
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.SessionContext{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readEntriesLocked()
	if err != nil {
		return agentruntime.SessionContext{}, err
	}
	projection := agentruntime.SessionContext{}
	var injectionNonce string
	for _, entry := range entries {
		switch entry.Kind {
		case SessionLogHeader:
			var payload SessionLogHeaderPayload
			if err := unmarshalEntryPayload(entry.Payload, &payload); err != nil {
				return agentruntime.SessionContext{}, fmt.Errorf("decode session header %d: %w", entry.Sequence, err)
			}
			if payload.MinReaderVersion == 0 {
				return agentruntime.SessionContext{}, fmt.Errorf(
					"decode session header %d: minReaderVer is required",
					entry.Sequence,
				)
			}
			projection.HasManifest = true
			projection.CanContinue = payload.MinReaderVersion <= currentEntryVersion
			injectionNonce = payload.InjectionNonce
		case SessionLogMessage:
			var payload SessionLogMessagePayload
			if err := unmarshalEntryPayload(entry.Payload, &payload); err != nil {
				return agentruntime.SessionContext{}, fmt.Errorf("decode message entry %d: %w", entry.Sequence, err)
			}
			message, include, err := fromLogMessage(payload)
			if err != nil {
				return agentruntime.SessionContext{}, fmt.Errorf("project message entry %d: %w", entry.Sequence, err)
			}
			if include {
				projection.Messages = append(projection.Messages, message)
			}
		case SessionLogArtifact:
			message, err := artifactMessage(entry.Payload, injectionNonce)
			if err != nil {
				return agentruntime.SessionContext{}, fmt.Errorf(
					"project context artifact entry %d: %w",
					entry.Sequence,
					err,
				)
			}
			projection.Messages = append(projection.Messages, message)
		}
	}
	return projection, nil
}

// FindArtifact locates a durable injection by its immutable key for exactly-once delivery reconciliation.
func (s *Store) FindArtifact(
	ctx context.Context,
	_ domain.AgentThreadID,
	injectionKey string,
) (*coresession.ArtifactRef, error) {
	if ctx == nil {
		return nil, errors.New("artifact lookup context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(injectionKey) == "" {
		return nil, errors.New("injection key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readEntriesLocked()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Kind != SessionLogArtifact {
			continue
		}
		var payload SessionLogContextArtifactPayload
		if err := unmarshalEntryPayload(entry.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode context artifact entry %d: %w", entry.Sequence, err)
		}
		if payload.IdempotencyKey == injectionKey {
			return &coresession.ArtifactRef{Sequence: entry.Sequence, EntryID: entry.ID}, nil
		}
	}
	return nil, nil
}

func artifactMessage(contents []byte, nonce string) (agentruntime.TurnMessage, error) {
	var payload SessionLogContextArtifactPayload
	if err := unmarshalEntryPayload(contents, &payload); err != nil {
		return agentruntime.TurnMessage{}, err
	}
	if strings.TrimSpace(nonce) == "" {
		return agentruntime.TurnMessage{}, errors.New("context artifact requires a session injection nonce")
	}
	switch payload.ArtifactKind {
	case "task_packet", "context_manifest", "briefing", "agent_result", "resume_package":
	default:
		return agentruntime.TurnMessage{}, fmt.Errorf("unknown context artifact kind %q", payload.ArtifactKind)
	}
	if len(payload.Body) == 0 {
		return agentruntime.TurnMessage{}, errors.New("context artifact body is required")
	}
	var body any
	if err := json.Unmarshal(payload.Body, &body); err != nil {
		return agentruntime.TurnMessage{}, fmt.Errorf("context artifact body is invalid JSON: %w", err)
	}
	if containsSensitiveValue(body) {
		return agentruntime.TurnMessage{}, errors.New("context artifact body contains a secret-bearing field")
	}
	return agentruntime.TurnMessage{
		Role: agentruntime.TurnRoleUser,
		Content: []agentruntime.TurnContentBlock{
			{
				Kind: agentruntime.TurnContentText,
				Text: "<" + payload.ArtifactKind + " session=\"" + nonce + "\">\n" + string(
					payload.Body,
				) + "\n</" + payload.ArtifactKind + ">",
			},
		},
	}, nil
}

// Close flushes and closes the file descriptor owned by this per-thread writer.
func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("session close context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync session log before close: %w", err)
	}
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close session log: %w", err)
	}
	s.file = nil
	return nil
}

func (s *Store) openForAppendLocked() error {
	if s.file != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create session log directory: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open session log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect session log: %w", err)
	}
	s.file = file
	return nil
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

func (s *Store) readEntriesLocked() ([]SessionLogEntry, error) {
	if s.file != nil {
		if err := s.file.Sync(); err != nil {
			return nil, fmt.Errorf("sync session log before read: %w", err)
		}
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open session log for read: %w", err)
	}
	defer file.Close()
	return decodeEntries(file)
}

func decodeEntries(reader io.Reader) ([]SessionLogEntry, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read session log: %w", err)
	}
	if len(contents) == 0 {
		return nil, nil
	}
	if contents[len(contents)-1] != '\n' {
		return nil, errors.New("session log has an incomplete tail")
	}
	lines := strings.Split(string(contents[:len(contents)-1]), "\n")
	entries := make([]SessionLogEntry, 0, len(lines))
	var sequence uint64
	for index, line := range lines {
		if line == "" {
			return nil, fmt.Errorf("session log line %d is empty", index+1)
		}
		var entry SessionLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("session log line %d is invalid: %w", index+1, err)
		}
		if err := validateEntry(entry, sequence); err != nil {
			return nil, fmt.Errorf("session log line %d: %w", index+1, err)
		}
		sequence = entry.Sequence
		entries = append(entries, entry)
	}
	return entries, nil
}

func validateEntry(entry SessionLogEntry, previous uint64) error {
	if strings.TrimSpace(entry.ID) == "" || entry.At.IsZero() || entry.Version == 0 {
		return errors.New("session entry identity, time and version are required")
	}
	if entry.Sequence != previous+1 {
		return fmt.Errorf("session sequence must advance from %d to %d", previous, previous+1)
	}
	if previous == 0 && entry.Kind != SessionLogHeader {
		return errors.New("first session entry must be session_header")
	}
	if !knownEntryKind(entry.Kind) {
		return fmt.Errorf("unknown session entry kind %q", entry.Kind)
	}
	var payload any
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return fmt.Errorf("session entry payload is invalid JSON: %w", err)
	}
	if containsSensitiveValue(payload) {
		return errors.New("session entry payload contains a secret-bearing field")
	}
	return nil
}

func knownEntryKind(kind SessionLogEntryKind) bool {
	switch kind {
	case SessionLogHeader, SessionLogRunStarted, SessionLogMessage, SessionLogToolStarted,
		SessionLogToolSettled, SessionLogQueueAdd, SessionLogQueueTake, SessionLogArtifact,
		SessionLogRunSettled, SessionLogInterrupted:
		return true
	default:
		return false
	}
}

var _ agentruntime.AgentSessionStore = (*Store)(nil)
var _ agentruntime.SessionFlusher = (*Store)(nil)
