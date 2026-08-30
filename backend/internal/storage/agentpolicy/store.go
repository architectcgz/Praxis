// Package agentpolicy persists the user-owned agent-policy document under
// DataRoot/config. It owns file validation and atomic replacement only; it
// never materializes a CapabilityGrant, which remains an orchestration concern.
package agentpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"praxis/internal/core/domain"
)

// Store guards one agent-policy.json file.
type Store struct {
	path  string
	mu    sync.RWMutex
	cache *domain.AgentPolicySnapshot
}

// NewStore returns a store for one DataRoot. Current loads lazily so an
// application can construct its composition graph before filesystem I/O.
func NewStore(path string) (*Store, error) {
	if filepath.Base(path) != "agent-policy.json" || !filepath.IsAbs(path) {
		return nil, errors.New("agent policy path must be an absolute agent-policy.json path")
	}
	return &Store{path: path}, nil
}

// Current returns the effective policy, including conservative defaults for
// profiles omitted from the user-owned JSON document.
func (s *Store) Current(ctx context.Context) (domain.AgentPolicySnapshot, error) {
	if ctx == nil {
		return domain.AgentPolicySnapshot{}, errors.New("agent policy context is required")
	}
	if err := ctx.Err(); err != nil {
		return domain.AgentPolicySnapshot{}, err
	}
	s.mu.RLock()
	if s.cache != nil {
		value := s.cache.Snapshot()
		s.mu.RUnlock()
		return value, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		loaded, err := load(s.path)
		if err != nil {
			return domain.AgentPolicySnapshot{}, err
		}
		s.cache = &loaded
	}
	return s.cache.Snapshot(), nil
}

// Update atomically persists a fully validated policy. The cache changes only
// after rename succeeds, so a failed write cannot widen later approvals.
func (s *Store) Update(ctx context.Context, snapshot domain.AgentPolicySnapshot) error {
	if ctx == nil {
		return errors.New("agent policy context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("contract error: invalid agent policy: %w", err)
	}
	document := documentFromSnapshot(snapshot)
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode agent policy: %w", err)
	}
	if err := writePrivateAtomically(s.path, append(encoded, '\n')); err != nil {
		return err
	}
	effective, err := snapshotFromDocument(document, revision(encoded))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = &effective
	s.mu.Unlock()
	return nil
}

// writePrivateAtomically replaces the policy file through a same-directory
// temporary file so a partial write can never become the active policy.
func writePrivateAtomically(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create policy directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agent-policy-*")
	if err != nil {
		return fmt.Errorf("create agent policy temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect agent policy temporary file: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write agent policy temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync agent policy temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close agent policy temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace agent policy: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect agent policy: %w", err)
	}
	return nil
}

var _ interface {
	Current(context.Context) (domain.AgentPolicySnapshot, error)
	Update(context.Context, domain.AgentPolicySnapshot) error
} = (*Store)(nil)
