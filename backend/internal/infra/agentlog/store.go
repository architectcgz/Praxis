// Package agentlog implements the target per-Agent append-only JSONL store.
package agentlog

import (
	"praxis/internal/contracts"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"praxis/internal/infra/dataroot"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

// Store owns one Agent transcript writer. It assigns every sequence number
// and fsyncs receipt boundaries before handing their references to core.
type Store struct {
	path         string
	temporaryDir string

	mu           sync.Mutex
	file         *os.File
	loaded       bool
	lastSequence uint64
	ids          system.IDGenerator
	clock        system.Clock
}

func Open(root dataroot.DataRoot, sessionID contracts.SessionID, agentID contracts.AgentID) (*Store, error) {
	if sessionID == "" || agentID == "" {
		return nil, errors.New("session and agent identifiers are required")
	}
	return NewStore(
		filepath.Join(root.Sessions, sessionID.String(), agentID.String()+".jsonl"),
		root.Temporary,
	)
}

func NewStore(path, temporaryDir string) (*Store, error) {
	return NewStoreWithSystem(path, temporaryDir, system.SecureIDGenerator{}, system.UTCClock{})
}

// NewStoreWithSystem creates a transcript store with explicitly supplied ID
// and clock capabilities.
func NewStoreWithSystem(path, temporaryDir string, ids system.IDGenerator, clock system.Clock) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return nil, errors.New("agent session path must be an absolute .jsonl path")
	}
	if !filepath.IsAbs(temporaryDir) {
		return nil, errors.New("agent session temporary directory must be absolute")
	}
	if ids == nil || clock == nil {
		return nil, errors.New("agent session system capabilities are required")
	}
	return &Store{path: filepath.Clean(path), temporaryDir: filepath.Clean(temporaryDir), ids: ids, clock: clock}, nil
}

func (s *Store) Initialize(ctx context.Context, header runtimecontract.AgentSessionHeader) error {
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
		DefinitionID:     header.DefinitionID.String(),
		Profile:          string(header.Profile),
		WorkspaceID:      header.WorkspaceID.String(),
		InjectionNonce:   header.InjectionNonce,
		MinReaderVersion: header.MinReaderVersion,
		WrittenBy:        header.WrittenBy,
	})
	if err != nil {
		return fmt.Errorf("encode agent session header: %w", err)
	}
	if err := s.appendLocked(entry{
		ID:      s.ids.New("event"),
		At:      s.clock.Now().UTC(),
		Kind:    entryHeader,
		Version: currentEntryVersion,
		Payload: payload,
	}); err != nil {
		return err
	}
	return s.syncLocked()
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

func (s *Store) verifyHeaderLocked(header runtimecontract.AgentSessionHeader) error {
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
		existing.DefinitionID != header.DefinitionID.String() ||
		existing.WorkspaceID != header.WorkspaceID.String() ||
		existing.InjectionNonce != header.InjectionNonce || existing.MinReaderVersion != header.MinReaderVersion {
		return errors.New("agent session header does not match immutable identity")
	}
	return nil
}

// CurrentSequence returns the last durable transcript sequence.
func (s *Store) CurrentSequence(ctx context.Context) (uint64, error) {
	if ctx == nil {
		return 0, errors.New("agent session sequence context is required")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return 0, err
	}
	return s.lastSequence, nil
}
