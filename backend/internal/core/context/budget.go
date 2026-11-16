package context

import (
	"crypto/sha256"
	"encoding/json"
	sessionentry "praxis/internal/core/entry"
)

// Budget 只在一次 Loop 中保存最近成功普通请求的用量锚点，不形成持久化校准状态。
type Budget struct {
	shape  [32]byte
	prefix [][32]byte
	input  int
	valid  bool
}

func visibleHashes(c ModelContext) ([][32]byte, error) {
	hashes := make([][32]byte, len(c.Entries))
	for i, e := range c.Entries {
		data, err := json.Marshal(struct {
			Role    string
			Content []sessionentry.ContentBlock
		}{Role: string(e.Role), Content: e.Content})
		if err != nil {
			return nil, err
		}
		hashes[i] = sha256.Sum256(data)
	}
	return hashes, nil
}
func shapeHash(c ModelContext, key string) [32]byte {
	return sha256.Sum256([]byte(key + "\x00" + c.SystemPrompt))
}
func (b *Budget) Estimate(c ModelContext, key string, overhead int) (int, error) {
	hashes, err := visibleHashes(c)
	if err != nil {
		return 0, err
	}
	shared := b.valid && b.shape == shapeHash(c, key) && len(hashes) >= len(b.prefix)
	if shared {
		for i, hash := range b.prefix {
			if hashes[i] != hash {
				shared = false
				break
			}
		}
	}
	if shared {
		increment := ModelContext{Entries: c.Entries[len(b.prefix):]}
		extra, err := increment.EstimatedTokens()
		if err != nil {
			return 0, err
		}
		if len(increment.Entries) == 0 {
			extra = 0
		}
		return b.input + extra, nil
	}
	b.valid = false
	input, err := c.EstimatedTokens()
	return input + overhead + 128, err
}
func (b *Budget) Observe(c ModelContext, key string, input int) {
	b.valid = false
	if input <= 0 {
		return
	}
	hashes, err := visibleHashes(c)
	if err != nil {
		return
	}
	b.shape = shapeHash(c, key)
	b.prefix = hashes
	b.input = input
	b.valid = true
}
func Margin(window int) int { return max(256, window/50) }
func CheckCapacity(input, output, window int) error {
	if window > 0 && input+output+Margin(window) > window {
		return &ContextWindowExceededError{EstimatedInputTokens: input + Margin(window), MaxOutputTokens: output, ContextWindow: window}
	}
	return nil
}
