package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	sessionentry "praxis/internal/core/session/entry"
)

// ModelContext 是一次 Agent 执行独占的派生上下文，Loop 在本次执行中更新它。
// Session 使用 Main Agent 的上下文，不另存副本；执行结束后不在历史中保留此对象。
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
