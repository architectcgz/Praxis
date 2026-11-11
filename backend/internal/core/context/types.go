package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	sessionentry "praxis/internal/core/session/entry"
)

// ModelContext 是本轮模型输入的只读引用视图，与 Session 共享 Entry 对象及内部载荷。
// 请求可以选择不同的条目列表，但不复制、改写或重排已接纳的历史。
type ModelContext struct {
	SystemPrompt string
	Entries      []*sessionentry.ContextEntry
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
