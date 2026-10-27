package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	sessionentry "praxis/internal/core/session/entry"
)

// ModelContext 是执行层从已接纳历史准备的独立模型视图，Loop 只消费当前轮次的值。
// 消息提交后由执行层准备下一轮视图，不作为 Run 或 Session 的另一份可变历史保存。
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
