package dataroot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"praxis/internal/system"
)

const backupManifestName = "manifest.json"

// BackupOptions controls an immutable, stopped-application DataRoot snapshot.
// The destination must not exist because replacing an operator-selected path
// would make rollback evidence ambiguous.
type BackupOptions struct {
	ConfirmedStopped bool
	Destination      string
	SnapshotID       string
	At               time.Time
	IDs              system.IDGenerator
	Clock            system.Clock
}

type BackupFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

// BackupManifest is safe to expose in diagnostic reports: it contains file
// identities and digests, never file contents.
type BackupManifest struct {
	SnapshotID string       `json:"snapshotId"`
	SourceRoot string       `json:"sourceRoot"`
	CreatedAt  time.Time    `json:"createdAt"`
	Files      []BackupFile `json:"files"`
}

// Backup copies the complete stopped DataRoot into a new directory and then
// atomically publishes it. SQLite sidecars, JSONL sessions, attachments, and
// configuration files are all included so a rollback never mixes snapshots.
func (d DataRoot) Backup(ctx context.Context, options BackupOptions) (BackupManifest, error) {
	if ctx == nil {
		return BackupManifest{}, errors.New("data root backup context is required")
	}
	if !options.ConfirmedStopped {
		return BackupManifest{}, errors.New("data root backup requires a stopped application")
	}
	if err := ctx.Err(); err != nil {
		return BackupManifest{}, err
	}
	if err := validateDataRootPath(d.Root); err != nil {
		return BackupManifest{}, err
	}
	sourceInfo, err := os.Stat(d.Root)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("inspect data root: %w", err)
	}
	if !sourceInfo.IsDir() {
		return BackupManifest{}, errors.New("data root is not a directory")
	}
	destinationInput := strings.TrimSpace(options.Destination)
	if destinationInput == "" {
		return BackupManifest{}, errors.New("backup destination is required")
	}
	destination, err := filepath.Abs(destinationInput)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("resolve backup destination: %w", err)
	}
	destination = filepath.Clean(destination)
	if err := validateDataRootPath(destination); err != nil {
		return BackupManifest{}, fmt.Errorf("backup destination: %w", err)
	}
	if sameOrNestedPath(d.Root, destination) || sameOrNestedPath(destination, d.Root) {
		return BackupManifest{}, errors.New("backup destination must be outside the data root")
	}
	if _, err := os.Stat(destination); err == nil {
		return BackupManifest{}, errors.New("backup destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return BackupManifest{}, fmt.Errorf("inspect backup destination: %w", err)
	}
	snapshotID := strings.TrimSpace(options.SnapshotID)
	ids := options.IDs
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	clock := options.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	if snapshotID == "" {
		snapshotID = ids.New("backup")
	}
	if !safeSnapshotID(snapshotID) {
		return BackupManifest{}, errors.New("backup snapshot id is invalid")
	}
	at := options.At
	if at.IsZero() {
		at = clock.Now().UTC()
	}
	if at.IsZero() {
		return BackupManifest{}, errors.New("backup timestamp is required")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return BackupManifest{}, fmt.Errorf("create backup parent: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".praxis-backup-*")
	if err != nil {
		return BackupManifest{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return BackupManifest{}, fmt.Errorf("protect backup staging directory: %w", err)
	}
	manifest := BackupManifest{
		SnapshotID: snapshotID,
		SourceRoot: d.Root,
		CreatedAt:  at.UTC(),
		Files:      make([]BackupFile, 0),
	}
	err = filepath.WalkDir(d.Root, func(sourcePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(d.Root, sourcePath)
		if err != nil {
			return fmt.Errorf("compute backup path: %w", err)
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in data root: %s", relative)
		}
		targetPath := filepath.Join(temporary, relative)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect backup file %s: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular data root entry: %s", relative)
		}
		copied, err := copyBackupFile(ctx, sourcePath, targetPath, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("copy backup file %s: %w", relative, err)
		}
		copied.Path = filepath.ToSlash(relative)
		manifest.Files = append(manifest.Files, copied)
		return nil
	})
	if err != nil {
		return BackupManifest{}, fmt.Errorf("copy data root backup: %w", err)
	}
	if err := writeBackupManifest(filepath.Join(temporary, backupManifestName), manifest); err != nil {
		return BackupManifest{}, err
	}
	if err := syncDirectory(temporary); err != nil {
		return BackupManifest{}, fmt.Errorf("sync backup staging directory: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		return BackupManifest{}, fmt.Errorf("publish data root backup: %w", err)
	}
	return manifest, nil
}

func safeSnapshotID(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func copyBackupFile(ctx context.Context, source, destination string, mode os.FileMode) (BackupFile, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return BackupFile{}, err
	}
	input, err := os.Open(source)
	if err != nil {
		return BackupFile{}, err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return BackupFile{}, err
	}
	defer func() { _ = output.Close() }()
	digest := sha256.New()
	writer := io.MultiWriter(output, digest)
	if _, err := io.Copy(writer, contextReader{ctx: ctx, reader: input}); err != nil {
		return BackupFile{}, err
	}
	if err := output.Sync(); err != nil {
		return BackupFile{}, err
	}
	if err := output.Close(); err != nil {
		return BackupFile{}, err
	}
	stat, err := os.Stat(destination)
	if err != nil {
		return BackupFile{}, err
	}
	return BackupFile{
		Size:   stat.Size(),
		SHA256: "sha256:" + hex.EncodeToString(digest.Sum(nil)),
		Mode:   uint32(mode.Perm()),
	}, nil
}

// contextReader aborts a long file copy as soon as the backup context is
// cancelled; io.Copy alone would run to completion.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func writeBackupManifest(path string, manifest BackupManifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create backup manifest: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync backup manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup manifest: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}
