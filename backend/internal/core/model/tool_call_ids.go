package model

import (
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"regexp"
)

var portableToolCallID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// BuildToolCallIDMap 为已校验的上下文建立请求级 ID 映射，调用与回执共用，原始上下文保持不变。
// 合法 ID 原样保留；替换值避开全部原始 ID 和已分配值，避免跨 Provider 编码产生碰撞。
func BuildToolCallIDMap(ctx appcontext.ModelContext) map[string]string {
	ids := make(map[string]string)
	used := make(map[string]bool)
	for _, entry := range ctx.Entries {
		for _, block := range entry.Content {
			if block.Kind == sessionentry.BlockToolCall || block.Kind == sessionentry.BlockToolResult {
				ids[block.CallID] = block.CallID
				used[block.CallID] = true
			}
		}
	}
	sequence := 1
	for _, entry := range ctx.Entries {
		for _, block := range entry.Content {
			if block.Kind != sessionentry.BlockToolCall && block.Kind != sessionentry.BlockToolResult {
				continue
			}
			if ids[block.CallID] != block.CallID || portableToolCallID.MatchString(block.CallID) {
				continue
			}
			for {
				encoded := fmt.Sprintf("call_%d", sequence)
				sequence++
				if used[encoded] {
					continue
				}
				ids[block.CallID] = encoded
				used[encoded] = true
				break
			}
		}
	}
	return ids
}
