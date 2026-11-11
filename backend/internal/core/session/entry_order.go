package session

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// orderToolResults 只整理实际读取范围内的引用，将中断恢复补写的回执归回声明。
// 不改变 Entry、ParentID 或日志顺序。
func orderToolResults(entries []*sessionentry.ContextEntry) []*sessionentry.ContextEntry {
	type callKey struct{ runID, callID string }
	calls := make(map[callKey]bool)
	results := make(map[callKey]*sessionentry.ContextEntry)
	for _, entry := range entries {
		for _, block := range entry.Content {
			switch block := block.(type) {
			case sessionentry.ToolCallBlock:
				calls[callKey{entry.RunID, block.CallID}] = true
			case sessionentry.ToolResultBlock:
				results[callKey{entry.RunID, block.CallID}] = entry
			}
		}
	}
	ordered := make([]*sessionentry.ContextEntry, 0, len(entries))
	placed := make(map[*sessionentry.ContextEntry]bool)
	for _, entry := range entries {
		pairedResult := false
		if entry.Role == role.Tool {
			for _, block := range entry.Content {
				if result, ok := block.(sessionentry.ToolResultBlock); ok && calls[callKey{entry.RunID, result.CallID}] {
					pairedResult = true
					break
				}
			}
		}
		if !pairedResult {
			ordered = append(ordered, entry)
		}
		for _, block := range entry.Content {
			if call, ok := block.(sessionentry.ToolCallBlock); ok {
				result := results[callKey{entry.RunID, call.CallID}]
				if result != nil && !placed[result] {
					ordered = append(ordered, result)
					placed[result] = true
				}
			}
		}
	}
	return ordered
}
