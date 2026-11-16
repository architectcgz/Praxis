package modelregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	modelconfig "praxis/internal/core/model/config"
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

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
