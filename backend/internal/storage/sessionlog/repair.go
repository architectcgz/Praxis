package sessionlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RepairResult describes an explicit tail repair; a false result means no file
// change was made, while malformed headers and middle lines return an error.
type RepairResult struct {
	Repaired bool
	Backup   string
}

// Repair removes only a malformed final line, preserves it in tmp, and appends
// an interruption marker. It never guesses through malformed durable history.
func (s *Store) Repair(ctx context.Context) (RepairResult, error) {
	if ctx == nil {
		return RepairResult{}, errors.New("session repair context is required")
	}
	if err := ctx.Err(); err != nil {
		return RepairResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			return RepairResult{}, fmt.Errorf("close session log before repair: %w", err)
		}
		s.file = nil
	}
	contents, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return RepairResult{}, nil
	}
	if err != nil {
		return RepairResult{}, fmt.Errorf("read session log for repair: %w", err)
	}
	if _, parseErr := decodeEntries(strings.NewReader(string(contents))); parseErr == nil {
		return RepairResult{}, nil
	} else if len(contents) == 0 || contents[len(contents)-1] == '\n' {
		return RepairResult{}, fmt.Errorf("session log has non-tail corruption: %w", parseErr)
	}
	complete, tail, err := splitCompleteTail(contents)
	if err != nil {
		return RepairResult{}, err
	}
	if len(tail) == 0 {
		return RepairResult{}, nil
	}
	entries, err := decodeCompleteEntries(complete)
	if err != nil {
		return RepairResult{}, err
	}
	if len(entries) == 0 {
		return RepairResult{}, errors.New("cannot repair a session without a complete header")
	}
	if err := os.MkdirAll(s.temporaryDir, 0o700); err != nil {
		return RepairResult{}, fmt.Errorf("create repair directory: %w", err)
	}
	backup := filepath.Join(s.temporaryDir, filepath.Base(s.path)+".partial-"+fmt.Sprint(time.Now().UTC().UnixNano()))
	if err := os.WriteFile(backup, tail, 0o600); err != nil {
		return RepairResult{}, fmt.Errorf("backup partial session tail: %w", err)
	}
	if err := os.WriteFile(s.path, complete, 0o600); err != nil {
		return RepairResult{}, fmt.Errorf("truncate partial session tail: %w", err)
	}
	s.loaded, s.lastSequence = true, 0
	if len(entries) > 0 {
		s.lastSequence = entries[len(entries)-1].Sequence
	}
	payload, err := json.Marshal(SessionLogOperationInterruptedPayload{
		Operation: "run",
		TargetID:  "session_tail",
		Note:      "tail_repaired",
	})
	if err != nil {
		return RepairResult{}, err
	}
	entry := SessionLogEntry{
		ID:       fmt.Sprintf("repair-%d", time.Now().UTC().UnixNano()),
		Sequence: s.lastSequence + 1,
		At:       time.Now().UTC(),
		Kind:     SessionLogInterrupted,
		Version:  currentEntryVersion,
		Payload:  payload,
	}
	if err := s.appendRepairEntryLocked(entry); err != nil {
		return RepairResult{}, err
	}
	return RepairResult{Repaired: true, Backup: backup}, nil
}

func (s *Store) appendRepairEntryLocked(entry SessionLogEntry) error {
	if err := validateEntry(entry, s.lastSequence); err != nil {
		return err
	}
	if err := s.openForAppendLocked(); err != nil {
		return err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := s.file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append repair interruption marker: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync repair interruption marker: %w", err)
	}
	s.lastSequence = entry.Sequence
	return nil
}

func splitCompleteTail(contents []byte) ([]byte, []byte, error) {
	if len(contents) == 0 || contents[len(contents)-1] == '\n' {
		return contents, nil, nil
	}
	index := strings.LastIndexByte(string(contents), '\n')
	if index < 0 {
		return nil, contents, nil
	}
	return contents[:index+1], contents[index+1:], nil
}

func decodeCompleteEntries(contents []byte) ([]SessionLogEntry, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	entries, err := decodeEntries(strings.NewReader(string(contents)))
	if err != nil {
		return nil, fmt.Errorf("session log has non-tail corruption: %w", err)
	}
	return entries, nil
}
