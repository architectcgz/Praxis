package modelregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	modelconfig "praxis/internal/core/model/config"
)

// ConfigurationError identifies the configuration file that prevented the
// registry from loading. Its cause never contains a resolved secret value.
type ConfigurationError struct {
	Path string
	Err  error
}

func (e *ConfigurationError) Error() string {
	if e == nil {
		return "configuration error"
	}
	return fmt.Sprintf("configuration %q: %v", e.Path, e.Err)
}

func (e *ConfigurationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// readModelConfigFile 解码并只读校验持久化的 canonical 配置，不清洗或静默修复字段。
func readModelConfigFile(path string) (modelconfig.ValidatedConfig, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		config := modelconfig.Config{
			Groups:    []modelconfig.Group{},
			Providers: []modelconfig.Provider{},
		}
		payload, marshalErr := json.MarshalIndent(config, "", "  ")
		if marshalErr != nil {
			return modelconfig.ValidatedConfig{}, fmt.Errorf("encode models config template: %w", marshalErr)
		}
		if writeErr := os.WriteFile(path, append(payload, '\n'), 0o600); writeErr != nil {
			return modelconfig.ValidatedConfig{}, fmt.Errorf("create models config: %w", writeErr)
		}
		file, err = os.Open(path)
	}
	if err != nil {
		return modelconfig.ValidatedConfig{}, fmt.Errorf("open models config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var config modelconfig.Config
	if err := decoder.Decode(&config); err != nil {
		return modelconfig.ValidatedConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return modelconfig.ValidatedConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	return modelconfig.NewValidatedConfig(config)
}

// readCredentialsFile reads the owner-only credential document. A missing
// document is created as an empty template so a fresh data root always has an
// editable auth file.
func readCredentialsFile(path string) (ProviderCredentials, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create auth config directory: %w", err)
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		document := ProviderCredentials{}
		payload, marshalErr := json.MarshalIndent(document, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("encode auth config template: %w", marshalErr)
		}
		if writeErr := os.WriteFile(path, append(payload, '\n'), 0o600); writeErr != nil {
			return nil, fmt.Errorf("create auth config: %w", writeErr)
		}
		file, err = os.Open(path)
	}
	if err != nil {
		return nil, fmt.Errorf("open auth config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	document := ProviderCredentials{}
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("auth config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, fmt.Errorf("auth config: invalid JSON: %w", err)
	}
	return document, nil
}

// writeConfigAtomically persists a complete replacement configuration without ever
// exposing a partially written file to startup or settings readers.
//
// The temporary file and same-directory rename preserve the previous valid
// document if writing fails. Mode stays 0600 because the auth document holds
// provider credentials and the model document is still owner-only config.
func writeConfigAtomically(path string, payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("flush temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
