package modelregistry

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

type modelKey struct {
	ProviderID string
	ModelID    string
}
