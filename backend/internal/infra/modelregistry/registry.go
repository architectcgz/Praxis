// Package modelregistry 加载并校验模型配置和凭据快照。
package modelregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	modelapi "praxis/internal/core/model"
	"praxis/internal/infra/providers/httpclient"
	"regexp"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ProviderCredential 表示凭据文档中的一项 Provider 凭据。
type ProviderCredential struct {
	Type CredentialType `json:"type"`
	Key  string         `json:"key"`
}

type CredentialType string

const CredentialTypeAPIKey CredentialType = "api_key"

// ProviderCredentials 将稳定的 Provider ID 映射到凭据。
// 凭据与模型配置分开持久化，使 Provider 元数据的读取、编辑和共享不会暴露密钥。
type ProviderCredentials map[string]ProviderCredential

// ConfigurationError 标识阻止加载的配置文件，底层原因不包含已解析的密钥值。
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

// CredentialSnapshot 只保存本次加载的凭据快照。
type CredentialSnapshot struct {
	keys map[string]string
}

func (s CredentialSnapshot) ProviderKey(providerID string) string {
	return s.keys[providerID]
}

// Registry 保存本次只读加载的模型配置与凭据快照，供装配期构造只读目录。
type Registry struct {
	config      modelapi.Document
	credentials ProviderCredentials
}

// Load 读取并校验模型配置与凭据文件，然后返回不可变快照。
func Load(modelsPath, credentialsPath string, client *http.Client) (*Registry, error) {
	config, err := readModelConfigFile(modelsPath)
	if err != nil {
		return nil, &ConfigurationError{
			Path: modelsPath,
			Err:  err,
		}
	}
	credentials, err := readCredentialsFile(credentialsPath)
	if err != nil {
		return nil, &ConfigurationError{
			Path: credentialsPath,
			Err:  err,
		}
	}
	credentials, err = validateCredentials(credentials)
	if err != nil {
		return nil, &ConfigurationError{
			Path: credentialsPath,
			Err:  err,
		}
	}
	for _, provider := range config.Providers {
		if _, err := httpclient.NewProxyClient(client, provider.ProxyURL); err != nil {
			return nil, &ConfigurationError{
				Path: modelsPath,
				Err:  fmt.Errorf("provider %q: %w", provider.ID, err),
			}
		}
	}
	return &Registry{
		config:      config,
		credentials: credentials,
	}, nil
}

// Config 返回本次加载的只读模型配置文档，调用方不得修改切片内容。
func (r *Registry) Config() modelapi.Document {
	return r.config
}

// Credentials 生成本次配置中已启用 Provider 的只读凭据快照。
func (r *Registry) Credentials() CredentialSnapshot {
	keys := make(map[string]string, len(r.config.Providers))
	for _, provider := range r.config.Providers {
		credential, exists := r.credentials[provider.ID]
		if exists && credential.Type == CredentialTypeAPIKey {
			keys[provider.ID] = credential.Key
		}
	}
	return CredentialSnapshot{keys: keys}
}

// readModelConfigFile 解码并只读校验持久化的 canonical 配置，不清洗或静默修复字段。
func readModelConfigFile(path string) (modelapi.Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return modelapi.Document{}, fmt.Errorf("open models config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var config modelapi.Document
	if err := decoder.Decode(&config); err != nil {
		return modelapi.Document{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return modelapi.Document{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := modelapi.ValidateDocument(config); err != nil {
		return modelapi.Document{}, err
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

// validateCredentials 只读校验凭据身份，原样复制密钥，不裁剪内容。
func validateCredentials(document ProviderCredentials) (ProviderCredentials, error) {
	validated := make(ProviderCredentials, len(document))
	for providerID, credential := range document {
		if !idPattern.MatchString(providerID) {
			return nil, fmt.Errorf("auth config: invalid provider id %q", providerID)
		}
		if credential.Type != CredentialTypeAPIKey {
			return nil, fmt.Errorf("auth config: provider %q: unsupported credential type %q", providerID, credential.Type)
		}
		if credential.Key == "" {
			return nil, fmt.Errorf("auth config: provider %q: credential key is required", providerID)
		}
		validated[providerID] = credential
	}
	return validated, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
