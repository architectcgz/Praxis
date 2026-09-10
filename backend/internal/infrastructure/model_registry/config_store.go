package modelregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

func readModelConfigFile(path string) (RegistryConfig, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		config := RegistryConfig{
			Groups: []GroupConfig{}, Providers: []ProviderConfig{},
		}
		payload, marshalErr := json.MarshalIndent(config, "", "  ")
		if marshalErr != nil {
			return RegistryConfig{}, fmt.Errorf("encode models config template: %w", marshalErr)
		}
		if writeErr := os.WriteFile(path, append(payload, '\n'), 0o600); writeErr != nil {
			return RegistryConfig{}, fmt.Errorf("create models config: %w", writeErr)
		}
		file, err = os.Open(path)
	}
	if err != nil {
		return RegistryConfig{}, fmt.Errorf("open models config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var config RegistryConfig
	if err := decoder.Decode(&config); err != nil {
		return RegistryConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return RegistryConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	return config, nil
}

// writeConfigAtomically persists a complete replacement configuration without ever
// exposing a partially written models file to startup or settings readers.
//
// The temporary file and same-directory rename preserve the previous valid
// configuration if writing fails. Mode stays 0600 because both files may hold
// provider credentials.
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
