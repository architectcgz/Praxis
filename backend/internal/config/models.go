package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"praxis/internal/model"
	"regexp"
	"slices"
	"strings"
)

// ConfigurationError 记录模型配置或角色定义加载失败的位置和底层原因。
type ConfigurationError struct {
	Path string
	Err  error
}

func (e *ConfigurationError) Error() string {
	return fmt.Sprintf("configuration %q: %v", e.Path, e.Err)
}

func (e *ConfigurationError) Unwrap() error { return e.Err }

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

// CredentialSnapshot 只保存本次加载的凭据快照。
type CredentialSnapshot struct {
	keys map[string]string
}

func (s CredentialSnapshot) ProviderKey(providerID string) string {
	return s.keys[providerID]
}

// ModelRegistry 保存本次只读加载的模型配置与凭据快照，供装配期构造只读目录。
type ModelRegistry struct {
	document    ModelDocument
	credentials ProviderCredentials
	proxies     map[string]Proxy
}

// Proxy 表示环境代理、显式地址或直连，地址在加载时已校验。
type Proxy struct {
	FromEnvironment bool
	URL             *url.URL
}

func (r *ModelRegistry) Proxies() map[string]Proxy { return r.proxies }

// LoadModels 读取并校验模型配置与凭据文件，然后返回不可变快照。
func LoadModels(modelsPath, credentialsPath string) (*ModelRegistry, error) {
	document, err := readModelConfigFile(modelsPath)
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
	proxies := make(map[string]Proxy, len(document.Providers))
	for _, provider := range document.Providers {
		proxy, err := parseProxy(provider.ProxyMode, provider.ProxyURL)
		if err != nil {
			return nil, &ConfigurationError{
				Path: modelsPath,
				Err:  fmt.Errorf("provider %q: %w", provider.ID, err),
			}
		}
		proxies[provider.ID] = proxy
	}
	return &ModelRegistry{
		document:    document,
		credentials: credentials,
		proxies:     proxies,
	}, nil
}

// Catalog 根据本次只读加载的配置构造按分组排序的只读模型目录。
func (r *ModelRegistry) Catalog() *model.Catalog {
	entries := make([]model.CatalogEntry, 0)
	var defaultOption model.Option
	ref := r.document.DefaultModel
	for _, provider := range r.document.Providers {
		for _, candidate := range provider.Models {
			option := model.Option{
				GroupID:               candidate.GroupID,
				ProviderID:            provider.ID,
				ModelID:               candidate.ID,
				Label:                 candidate.DisplayName,
				ProviderName:          provider.DisplayName,
				ReasoningLevels:       candidate.ReasoningLevels,
				DefaultReasoningLevel: candidate.DefaultReasoningLevel,
			}
			// 未指定默认值时按文件顺序选择，不受展示排序影响。
			if (ref == nil && len(entries) == 0) || (ref != nil && ref.ProviderID == provider.ID && ref.ModelID == candidate.ID) {
				defaultOption = option
			}
			entries = append(entries, model.CatalogEntry{
				Option: option,
				Config: model.Config{
					ProviderID:      provider.ID,
					ModelID:         candidate.ID,
					ReasoningLevel:  candidate.DefaultReasoningLevel,
					ReasoningLevels: candidate.ReasoningLevels,
					Thinking:        candidate.Thinking,
					APIFormat:       string(candidate.APIFormat),
					ContextWindow:   candidate.ContextWindow,
					MaxOutputTokens: candidate.MaxOutputTokens,
					BaseURL:         provider.BaseURL,
				},
			})
		}
	}
	orderCatalogEntries(entries, r.document.Groups)
	return model.NewCatalog(entries, defaultOption)
}

func orderCatalogEntries(entries []model.CatalogEntry, groups []ModelGroup) {
	groupOrder := make(map[string]int, len(groups))
	for index, group := range groups {
		groupOrder[group.ID] = index
	}
	slices.SortStableFunc(entries, func(left, right model.CatalogEntry) int {
		leftGroup, leftOK := groupOrder[left.Option.GroupID]
		rightGroup, rightOK := groupOrder[right.Option.GroupID]
		if leftOK && rightOK && leftGroup != rightGroup {
			return leftGroup - rightGroup
		}
		if leftOK != rightOK {
			if leftOK {
				return -1
			}
			return 1
		}
		if left.Option.Label == right.Option.Label {
			if left.Option.ProviderID == right.Option.ProviderID {
				return strings.Compare(left.Option.ModelID, right.Option.ModelID)
			}
			return strings.Compare(left.Option.ProviderID, right.Option.ProviderID)
		}
		return strings.Compare(left.Option.Label, right.Option.Label)
	})
}

// Credentials 生成本次配置中已启用 Provider 的只读凭据快照。
func (r *ModelRegistry) Credentials() CredentialSnapshot {
	keys := make(map[string]string, len(r.document.Providers))
	for _, provider := range r.document.Providers {
		credential, exists := r.credentials[provider.ID]
		if exists && credential.Type == CredentialTypeAPIKey {
			keys[provider.ID] = credential.Key
		}
	}
	return CredentialSnapshot{keys: keys}
}

// readModelConfigFile 解码并只读校验持久化的 canonical 配置，不清洗或静默修复字段。
func readModelConfigFile(path string) (ModelDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return ModelDocument{}, fmt.Errorf("open models config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var document ModelDocument
	if err := decoder.Decode(&document); err != nil {
		return ModelDocument{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return ModelDocument{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := validateModelDocument(document); err != nil {
		return ModelDocument{}, err
	}
	return document, nil
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
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}
