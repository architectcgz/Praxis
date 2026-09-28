package modelregistry

import (
	"errors"
)

// ApplyConfig 校验并原子写入模型配置，然后原地替换内存索引。运行时模型构建器
// 与 binding 共用同一个 Registry 指针；原地替换可使保存后的配置立即对新 execution 生效，
// 已创建 execution 仍只使用自身保存的模型快照。校验失败时不写入文件，也不修改内存。
func (r *Registry) ApplyConfig(config RegistryConfig) error {
	validated, err := Prepare(config)
	if err != nil {
		return err
	}
	return r.ApplyValidatedConfig(validated)
}

// ApplyValidatedConfig 原子持久化已准备的配置并替换内存索引。
func (r *Registry) ApplyValidatedConfig(validated ValidatedConfig) error {
	if r == nil {
		return errors.New("registry is not initialized")
	}
	if !validated.prepared {
		return errors.New("model configuration has not been prepared")
	}
	nextConfig := validated.config
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
