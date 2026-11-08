package provider

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
			if callID, ok := toolCallID(block); ok {
				ids[callID] = callID
				used[callID] = true
			}
		}
	}
	sequence := 1
	for _, entry := range ctx.Entries {
		for _, block := range entry.Content {
			callID, ok := toolCallID(block)
			if !ok {
				continue
			}
			if ids[callID] != callID || portableToolCallID.MatchString(callID) {
				continue
			}
			for {
				encoded := fmt.Sprintf("call_%d", sequence)
				sequence++
				if used[encoded] {
					continue
				}
				ids[callID] = encoded
				used[encoded] = true
				break
			}
		}
	}
	return ids
}

func toolCallID(block sessionentry.ContentBlock) (string, bool) {
	switch value := block.(type) {
	case sessionentry.ToolCallBlock:
		return value.CallID, true
	case sessionentry.ToolResultBlock:
		return value.CallID, true
	default:
		return "", false
	}
}
