package request

import (
	"praxis/internal/contracts"
	"regexp"
	"strings"
)

// SetProviderKey 保存规范化的 Provider 身份和原始密钥。
type SetProviderKey struct {
	ProviderID string
	Key        string
}

var providerKeyIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func NewSetProviderKey(providerID, key string) (SetProviderKey, error) {
	providerID = strings.TrimSpace(providerID)
	if !providerKeyIDPattern.MatchString(providerID) {
		return SetProviderKey{}, contracts.InvalidValue("providerId", "invalid provider id")
	}
	if key != "" && strings.TrimSpace(key) == "" {
		return SetProviderKey{}, contracts.InvalidValue("key", "API key cannot contain only whitespace")
	}
	return SetProviderKey{ProviderID: providerID, Key: key}, nil
}
