package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domaincontext "praxis/internal/domain/context"
	domainfoundation "praxis/internal/domain/foundation"

	"praxis/internal/storage/dataroot"
)

// AttachmentStore owns opaque durable tool-result attachments.
type AttachmentStore struct{ root, temporary string }

func NewAttachmentStore(root dataroot.DataRoot) *AttachmentStore {
	return &AttachmentStore{root: root.Attachments, temporary: root.Temporary}
}

// Write stores a content-addressed attachment with a short, trusted identifier.
func (s *AttachmentStore) Write(ctx context.Context, id string, contents []byte) (domaincontext.ContentRef, error) {
	if err := checkContext(ctx); err != nil {
		return domaincontext.ContentRef{}, err
	}
	if !safeFileID(id) {
		return domaincontext.ContentRef{}, errors.New("attachment id is invalid")
	}
	if containsSecretText(contents) {
		return domaincontext.ContentRef{}, errors.New("refusing to store secret-like attachment content")
	}
	path := filepath.Join(s.root, id+".blob")
	if err := writeAtomically(path, s.temporary, contents); err != nil {
		return domaincontext.ContentRef{}, err
	}
	sum := sha256.Sum256(contents)
	return domaincontext.NewContentRef(
		domaincontext.ContentRefArtifact,
		path,
		"sha256:"+hex.EncodeToString(sum[:]),
		"",
		int64(len(contents)),
	)
}

func (s *AttachmentStore) Read(ctx context.Context, ref domaincontext.ContentRef) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if !within(s.root, ref.Path) {
		return nil, errors.New("attachment path is outside the attachment root")
	}
	contents, err := os.ReadFile(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	return contents, nil
}

// NoteFileStore owns Markdown bodies while SQLite stores their metadata.
type NoteFileStore struct{ root, temporary string }

func NewNoteFileStore(root dataroot.DataRoot) *NoteFileStore {
	return &NoteFileStore{root: root.Notes, temporary: root.Temporary}
}

func (s *NoteFileStore) Write(ctx context.Context, id domainfoundation.NoteID, contents []byte) (domaincontext.ContentRef, error) {
	if err := checkContext(ctx); err != nil {
		return domaincontext.ContentRef{}, err
	}
	if !safeFileID(id.String()) {
		return domaincontext.ContentRef{}, errors.New("note id is invalid")
	}
	if containsSecretText(contents) {
		return domaincontext.ContentRef{}, errors.New("refusing to store secret-like note content")
	}
	path := filepath.Join(s.root, id.String()+".md")
	if err := writeAtomically(path, s.temporary, contents); err != nil {
		return domaincontext.ContentRef{}, err
	}
	sum := sha256.Sum256(contents)
	return domaincontext.NewContentRef(
		domaincontext.ContentRefArtifact,
		path,
		"sha256:"+hex.EncodeToString(sum[:]),
		"",
		int64(len(contents)),
	)
}

func (s *NoteFileStore) Read(ctx context.Context, ref domaincontext.ContentRef) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if !within(s.root, ref.Path) {
		return nil, errors.New("note path is outside the note root")
	}
	contents, err := os.ReadFile(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("read note: %w", err)
	}
	return contents, nil
}

func writeAtomically(path, temporary string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}
	if err := os.MkdirAll(temporary, 0o700); err != nil {
		return fmt.Errorf("create blob temporary directory: %w", err)
	}
	file, err := os.CreateTemp(temporary, ".praxis-blob-*")
	if err != nil {
		return fmt.Errorf("create blob temporary file: %w", err)
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect blob temporary file: %w", err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write blob temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync blob temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close blob temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace blob: %w", err)
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("blob context is required")
	}
	return ctx.Err()
}
func safeFileID(id string) bool {
	return id != "" && len(id) <= 192 && !strings.ContainsAny(id, "/\\\x00\r\n")
}
func within(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func containsSecretText(contents []byte) bool {
	lower := strings.ToLower(string(contents))
	return strings.Contains(lower, "authorization:") || strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "api_key=")
}
