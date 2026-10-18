package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	modelconfig "praxis/internal/model/config"
)

// ConfigurationError 标识阻止注册表加载的配置文件，
// 其底层原因不包含已解析的密钥值。
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
func readModelConfigFile(path string) (modelconfig.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return modelconfig.Config{}, fmt.Errorf("open models config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var config modelconfig.Config
	if err := decoder.Decode(&config); err != nil {
		return modelconfig.Config{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return modelconfig.Config{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := modelconfig.Validate(config); err != nil {
		return modelconfig.Config{}, err
	}
	return config, nil
}

// readCredentialsFile 只读加载凭据文件，不创建目录或缺失文件。
func readCredentialsFile(path string) (ProviderCredentials, error) {
	file, err := os.Open(path)
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

// writeConfigAtomically 完整替换持久化配置，确保启动和设置读取方不会读到部分写入的文件。
//
// 通过临时文件和同目录重命名，在写入失败时保留之前的有效文档。
// 文件权限保持 0600，因为凭据文档包含 Provider 密钥，模型配置也仅允许所有者访问。
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
