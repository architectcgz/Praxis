package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	sessionentry "praxis/internal/core/session/entry"
)

// ModelContext 是从当前 Context 构建的本轮独立模型视图，包含本次执行的提示词。
// 请求视图不直接共享 Session 或子执行持有的可变上下文条目。
type ModelContext struct {
	SystemPrompt string
	Entries      []sessionentry.ContextEntry
}

// Clone 返回 ModelContext 的独立副本。
func (c ModelContext) Clone() ModelContext {
	entries := make([]sessionentry.ContextEntry, len(c.Entries))
	for index, entry := range c.Entries {
		entries[index] = entry.Clone()
	}
	c.Entries = entries
	return c
}

// Digest 返回当前完整模型上下文的内容摘要。
func (c ModelContext) Digest() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256-" + hex.EncodeToString(digest[:]), nil
}
