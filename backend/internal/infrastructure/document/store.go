// Package document stores immutable JSON documents outside the relational database.
package document

import (
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
)

// Store writes content-addressed JSON documents. A document is published
// before its SQLite reference is committed, so a failed transaction leaves
// only an unreachable document and never a database reference to missing data.
type Store struct {
	root   string
	memory map[string][]byte
	mu     sync.RWMutex
}

// New creates a file-backed document store rooted at an absolute directory.
func New(root string) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		return nil, errors.New("document root must be an absolute non-root path")
	}
	return &Store{root: filepath.Clean(root)}, nil
}

// NewMemory creates an in-memory store for isolated tests and embedded users.
func NewMemory() *Store {
	return &Store{memory: make(map[string][]byte)}
}

// Put validates and atomically publishes one immutable JSON document.
func (s *Store) Put(ctx context.Context, collection, id string, value any) (string, error) {
	if ctx == nil {
		return "", errors.New("document write context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !safePart(collection) || !safePart(id) {
		return "", errors.New("document collection and id are invalid")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode document: %w", err)
	}
	var checked any
	if err := json.Unmarshal(encoded, &checked); err != nil {
		return "", fmt.Errorf("inspect document: %w", err)
	}
	if containsSensitiveValue(checked) {
		return "", errors.New("document contains a sensitive field")
	}
	digest := sha256.Sum256(encoded)
	name := "sha256-" + hex.EncodeToString(digest[:])
	ref := filepath.ToSlash(filepath.Join(collection, id, name+".json"))

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.memory != nil {
		if _, exists := s.memory[ref]; !exists {
			s.memory[ref] = append([]byte(nil), encoded...)
		}
		return ref, nil
	}
	destination := filepath.Join(s.root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", fmt.Errorf("create document directory: %w", err)
	}
	if _, err := os.Stat(destination); err == nil {
		return ref, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect document: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".document-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create document temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("protect document temporary file: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write document: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync document: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close document temporary file: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return "", fmt.Errorf("publish document: %w", err)
	}
	return ref, nil
}

// Get loads and validates one document referenced by a SQLite row.
func (s *Store) Get(ctx context.Context, ref string, target any) error {
	if ctx == nil {
		return errors.New("document read context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if target == nil || !safeReference(ref) {
		return errors.New("document reference is invalid")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var encoded []byte
	if s.memory != nil {
		value, ok := s.memory[filepath.ToSlash(ref)]
		if !ok {
			return os.ErrNotExist
		}
		encoded = append([]byte(nil), value...)
	} else {
		value, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(ref)))
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		if err != nil {
			return fmt.Errorf("read document: %w", err)
		}
		encoded = value
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("decode document: %w", err)
	}
	return nil
}

func safePart(value string) bool {
	return value != "" && value != "." && value != ".." &&
		filepath.Base(value) == value && !strings.ContainsAny(value, "/\\:\x00\r\n")
}

func safeReference(value string) bool {
	value = filepath.ToSlash(strings.TrimSpace(value))
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	parts := strings.Split(value, "/")
	return len(parts) == 3 && safePart(parts[0]) && safePart(parts[1]) &&
		strings.HasPrefix(parts[2], "sha256-") && strings.HasSuffix(parts[2], ".json")
}

func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "apikey", "api_key", "authorization", "secret", "token", "providerpayload", "provider_payload", "hiddenprompt", "hidden_prompt":
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
