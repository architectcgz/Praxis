// Package dataroot owns the local application-root path layout. It is the
// shared base every other storage adapter resolves its files against, so it
// deliberately depends on nothing else in the storage layer.
package dataroot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const dataRootEnvironment = "PRAXIS_DATA_ROOT"

// DataRoot contains every local product-data path. Configuration, secrets,
// projects, and runtime state have separate ownership boundaries below Root.
// This type only resolves paths; the provider registry is the sole component
// that reads or writes the model and secret files they point at.
type DataRoot struct {
	Root            string
	Config          string
	Secrets         string
	Projects        string
	Runtime         string
	Database        string
	Sessions        string
	Attachments     string
	Notes           string
	Temporary       string
	ModelsConfig    string
	SecretsConfig   string
	AgentPolicyFile string
}

// Resolve chooses the explicit root, then PRAXIS_DATA_ROOT, then the current
// user's .praxis directory. It deliberately does not create files.
func Resolve(root string) (DataRoot, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = strings.TrimSpace(os.Getenv(dataRootEnvironment))
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return DataRoot{}, fmt.Errorf("resolve user home directory: %w", err)
		}
		root = filepath.Join(home, ".praxis")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return DataRoot{}, fmt.Errorf("resolve data root: %w", err)
	}
	root = filepath.Clean(absolute)
	if root == string(filepath.Separator) {
		return DataRoot{}, errors.New("data root cannot be the filesystem root")
	}
	config := filepath.Join(root, "config")
	secrets := filepath.Join(root, "secrets")
	runtime := filepath.Join(root, "runtime")
	return DataRoot{
		Root:            root,
		Config:          config,
		Secrets:         secrets,
		Projects:        filepath.Join(root, "projects"),
		Runtime:         runtime,
		Database:        filepath.Join(runtime, "praxis.db"),
		Sessions:        filepath.Join(runtime, "agent-sessions"),
		Attachments:     filepath.Join(runtime, "attachments"),
		Notes:           filepath.Join(runtime, "notes"),
		Temporary:       filepath.Join(runtime, "tmp"),
		ModelsConfig:    filepath.Join(config, "models.json"),
		SecretsConfig:   filepath.Join(secrets, "secrets.json"),
		AgentPolicyFile: filepath.Join(config, "agent-policy.json"),
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
	for _, path := range []string{
		d.Root,
		d.Config,
		d.Secrets,
		d.Projects,
		d.Runtime,
		d.Sessions,
		d.Attachments,
		d.Notes,
		d.Temporary,
	} {
		if strings.TrimSpace(path) == "" {
			return errors.New("data root contains an empty required path")
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create data directory %q: %w", path, err)
		}
	}
	return nil
}

func validateDataRootPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) == string(filepath.Separator) {
		return errors.New("data root path must be an absolute non-root path")
	}
	return nil
}

func sameOrNestedPath(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
