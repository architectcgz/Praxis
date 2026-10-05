// Package dataroot owns the local application-root path layout. It is the
// shared base every other infrastructure adapter resolves its files against,
// so it deliberately depends on nothing else in the infrastructure layer.
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

// DataRoot contains every local product-data path. Configuration, projects,
// and runtime state have separate ownership boundaries below Root. This type
// only resolves paths; the provider registry is the sole component that reads
// or writes the model and credential files they point at.
type DataRoot struct {
	Root                 string
	Config               string
	Projects             string
	Runtime              string
	Documents            string
	Attachments          string
	Temporary            string
	ModelProvidersConfig string
	ModelCredentialsFile string
	ToolConfig           string
	AgentDefinitions     string
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
	runtime := filepath.Join(root, "runtime")
	return DataRoot{
		Root:                 root,
		Config:               config,
		Projects:             filepath.Join(root, "projects"),
		Runtime:              runtime,
		Documents:            filepath.Join(runtime, "documents"),
		Attachments:          filepath.Join(runtime, "attachments"),
		Temporary:            filepath.Join(runtime, "tmp"),
		ModelProvidersConfig: filepath.Join(config, "models.json"),
		ModelCredentialsFile: filepath.Join(config, "auth.json"),
		ToolConfig:           filepath.Join(config, "tools.json"),
		AgentDefinitions:     filepath.Join(config, "agents"),
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
		d.Projects,
		d.Runtime,
		d.Documents,
		d.AgentDefinitions,
		d.Attachments,
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
