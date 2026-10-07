package modelregistry

import (
	"errors"

	modelconfig "praxis/internal/core/model/config"
)

// Save 原子持久化输入边界已准备的配置并替换内存索引，不再次规范化或业务校验。
// 零值配置直接拒绝；持久化失败保留原始错误分类，不替换运行时配置。
func (r *Registry) Save(validated modelconfig.ValidatedConfig) error {
	if r == nil {
		return errors.New("registry is not initialized")
	}
	if !validated.Prepared() {
		return errors.New("model configuration has not been prepared")
	}
	nextConfig := validated.Config()
	providerClients, err := buildProviderClients(nextConfig.Providers, r.client)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	nextCredentials := retainCredentialsForProviders(r.credentials, nextConfig.Providers)
	modelsByKey, providersByID := buildIndexes(nextConfig.Providers)
	if credentialsChanged(r.credentials, nextCredentials) && r.credentialsPath != "" {
		if err := writeConfigAtomically(r.credentialsPath, nextCredentials); err != nil {
			return &ConfigurationError{Path: r.credentialsPath, Err: err}
		}
	}
	if err := writeConfigAtomically(r.modelsPath, nextConfig); err != nil {
		return &ConfigurationError{Path: r.modelsPath, Err: err}
	}
	r.config = nextConfig
	r.credentials = nextCredentials
	r.modelsByKey = modelsByKey
	r.providersByID = providersByID
	r.providerClients = providerClients
	return nil
}

var _ modelconfig.ConfigManager = (*Registry)(nil)
