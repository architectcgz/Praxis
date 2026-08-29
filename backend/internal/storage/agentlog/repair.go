package agentlog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
