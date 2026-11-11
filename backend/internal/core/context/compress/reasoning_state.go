package compress

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// summaryEntry 只承载摘要模型需要的角色和正文，不复制会话条目的身份、计时及其他执行事实。
type summaryEntry struct {
	Role    role.Role                   `json:"role"`
	Content []sessionentry.ContentBlock `json:"content"`
}

// summaryEntries 不把签名、加密载荷或展示性思考正文发送给摘要模型。
func summaryEntries(entries []*sessionentry.ContextEntry) []summaryEntry {
	var selected []summaryEntry
	for _, entry := range entries {
		value := summaryEntry{Role: entry.Role}
		for _, block := range entry.Content {
			switch block.(type) {
			case sessionentry.ReasoningStateBlock, sessionentry.ThinkingBlock:
			default:
				value.Content = append(value.Content, block)
			}
		}
		if len(value.Content) > 0 {
			selected = append(selected, value)
		}
	}
	return selected
}
