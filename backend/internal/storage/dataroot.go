package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const dataRootEnvironment = "PRAXIS_DATA_ROOT"

// DataRoot contains every local product-data path. Only the future secret
// adapter may write secret values, so this type exposes its path but never reads it.
type DataRoot struct {
	Root            string
	Database        string
	Sessions        string
	Attachments     string
	Notes           string
	Temporary       string
	ModelsConfig    string
	SecretsConfig   string
	AgentPolicyFile string
}

// ResolveDataRoot chooses the explicit root, then PRAXIS_DATA_ROOT, then the
// per-user configuration directory. It deliberately does not create files.
func ResolveDataRoot(root string) (DataRoot, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = strings.TrimSpace(os.Getenv(dataRootEnvironment))
	}
	if root == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return DataRoot{}, fmt.Errorf("resolve user configuration directory: %w", err)
		}
		root = filepath.Join(configDir, "Praxis")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return DataRoot{}, fmt.Errorf("resolve data root: %w", err)
	}
	root = filepath.Clean(absolute)
	if root == string(filepath.Separator) {
		return DataRoot{}, errors.New("data root cannot be the filesystem root")
	}
	return DataRoot{
		Root:            root,
		Database:        filepath.Join(root, "praxis.db"),
		Sessions:        filepath.Join(root, "agent-sessions"),
		Attachments:     filepath.Join(root, "attachments"),
		Notes:           filepath.Join(root, "notes"),
		Temporary:       filepath.Join(root, "tmp"),
		ModelsConfig:    filepath.Join(root, "models.json"),
		SecretsConfig:   filepath.Join(root, "secrets.json"),
		AgentPolicyFile: filepath.Join(root, "agent-policy.json"),
	}, nil
}

// Initialize creates the non-secret directory structure with owner-only access.
func (d DataRoot) Initialize(ctx context.Context) error {
	if ctx == nil {
		return errors.New("data root initialization context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, path := range []string{d.Root, d.Sessions, d.Attachments, d.Notes, d.Temporary} {
		if strings.TrimSpace(path) == "" {
			return errors.New("data root contains an empty required path")
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create data directory %q: %w", path, err)
		}
	}
	return nil
}
