package modelregistry

import (
	modelconfig "praxis/internal/core/model/config"
)

// Save 原子持久化输入边界已准备的配置并更新管理端状态，不再次规范化或业务校验。
// 持久化失败保留原始错误分类，不替换内存编辑状态；保存独立副本以隔离调用方修改。
func (r *Registry) Save(config modelconfig.Config) error {
	nextConfig := config.Clone()
	providerClients, err := buildProviderClients(nextConfig.Providers, r.client)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	nextCredentials := retainCredentialsForProviders(r.credentials, nextConfig.Providers)
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
	r.providersByID = indexProviders(nextConfig.Providers)
	r.providerClients = providerClients
	return nil
}

var _ modelconfig.ConfigManager = (*Registry)(nil)
