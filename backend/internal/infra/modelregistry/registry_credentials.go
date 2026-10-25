package modelregistry

import (
	"fmt"
	modelconfig "praxis/internal/core/model/config"
	"regexp"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validateCredentials 只读校验凭据身份，原样复制密钥，不裁剪内容。
//
// 保留不在当前模型配置中的 Provider 条目：两个文档独立管理，
// 凭据仅在已配置的 Provider 按 ID 解析时生效。
func validateCredentials(document ProviderCredentials) (ProviderCredentials, error) {
	validated := make(ProviderCredentials, len(document))
	for providerID, credential := range document {
		id := providerID
		if !idPattern.MatchString(id) {
			return nil, fmt.Errorf("auth config: invalid provider id %q", id)
		}
		if credential.Type != CredentialTypeAPIKey {
			return nil, fmt.Errorf("auth config: provider %q: unsupported credential type %q", id, credential.Type)
		}
		if credential.Key == "" {
			return nil, fmt.Errorf("auth config: provider %q: credential key is required", id)
		}
		validated[id] = credential
	}
	return validated, nil
}

// SetProviderKey 保存或清除输入边界已校验的 Provider API 密钥。
// 在锁内检查 Provider 是否仍存在；只写凭据文档，模型配置文档不包含密钥。
func (r *Registry) SetProviderKey(providerID, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providersByID[providerID]; !exists {
		return fmt.Errorf("provider %q is not configured", providerID)
	}
	credentials := cloneCredentials(r.credentials)
	if value == "" {
		delete(credentials, providerID)
	} else {
		credentials[providerID] = ProviderCredential{Type: CredentialTypeAPIKey, Key: value}
	}
	if !credentialsChanged(r.credentials, credentials) {
		return nil
	}
	if r.credentialsPath != "" {
		if err := writeConfigAtomically(r.credentialsPath, credentials); err != nil {
			return &ConfigurationError{Path: r.credentialsPath, Err: err}
		}
	}
	r.credentials = credentials
	return nil
}

// ProviderKey 返回已配置 Provider 保存的 API 密钥。
func (r *Registry) ProviderKey(providerID string) string {
	if !idPattern.MatchString(providerID) {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, exists := r.providersByID[providerID]; !exists {
		return ""
	}
	return r.credentialKeyLocked(providerID)
}

// HasProviderKey 报告密钥是否存在，不向应用层或 binding 层暴露密钥值。
func (r *Registry) HasProviderKey(providerID string) bool {
	return r.ProviderKey(providerID) != ""
}

// credentialKeyLocked 解析 Provider 当前有效的 API 密钥。
// 调用方必须持有 r.mu；仅允许运行时解析路径读取密钥。
func (r *Registry) credentialKeyLocked(providerID string) string {
	credential, exists := r.credentials[providerID]
	if !exists || credential.Type != CredentialTypeAPIKey {
		return ""
	}
	return credential.Key
}

func cloneCredentials(credentials ProviderCredentials) ProviderCredentials {
	cloned := make(ProviderCredentials, len(credentials)+1)
	for providerID, credential := range credentials {
		cloned[providerID] = credential
	}
	return cloned
}

// retainCredentialsForProviders 返回属于已配置 Provider 的凭据，
// 因此保存下一份模型配置时也会删除已移除 Provider 的密钥。
// 未保存凭据的 Provider 保持缺省，不创建空条目。
func retainCredentialsForProviders(credentials ProviderCredentials, providers []modelconfig.Provider) ProviderCredentials {
	retained := make(ProviderCredentials, len(providers))
	for _, provider := range providers {
		if credential, exists := credentials[provider.ID]; exists {
			retained[provider.ID] = credential
		}
	}
	return retained
}

func credentialsChanged(current, next ProviderCredentials) bool {
	if len(current) != len(next) {
		return true
	}
	for providerID, credential := range next {
		existing, exists := current[providerID]
		if !exists || existing != credential {
			return true
		}
	}
	return false
}
