package execution

import (
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"slices"
	"strings"
)

// contextContent 将已选业务数据转换为通用有序条目；当前输入和共享事实保留原文。
func contextContent(systemPrompt, inputEntryID string, shared []sessionmodel.SharedFact, entries []sessionentry.ContextEntry) (appcontext.Content, error) {
	content := appcontext.Content{SystemPrompt: systemPrompt}
	shared = slices.Clone(shared)
	slices.SortFunc(shared, func(a, b sessionmodel.SharedFact) int {
		if order := a.CreatedAt.Compare(b.CreatedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	for _, fact := range shared {
		content.Entries = append(content.Entries, sessionentry.ContextEntry{
			Kind: sessionentry.ContextEntryReference, Role: sessionentry.RoleUser, Protected: true,
			Content: []sessionentry.Block{{Kind: sessionentry.BlockText, Text: fmt.Sprintf("[Session context: %s]\n%s", fact.Kind, fact.Content)}},
		})
	}
	selected := historyEntries(entries)
	found := false
	for index := range selected {
		if selected[index].ID == inputEntryID && selected[index].Role == sessionentry.RoleUser {
			selected[index].Protected = true
			found = true
		}
	}
	if !found {
		return appcontext.Content{}, errors.New("current input user entry is missing")
	}
	content.Entries = append(content.Entries, selected...)
	return content, nil
}

// modelVisibleEntry 选择模型可见块，保留原 Entry 身份及执行归属。
func modelVisibleEntry(value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool) {
	content := make([]sessionentry.Block, 0, len(value.Content))
	for _, block := range value.Content {
		if block.Kind != sessionentry.BlockThinking {
			content = append(content, block)
		}
	}
	value.Content = content
	return value, len(content) > 0
}

// historyEntries 把回执关联到对应调用后，恢复补写和执行期间入队的用户消息不打断工具交互。
// 只调整模型投影，不改写日志顺序；使用 Execution 和 call ID 避免跨任务误配同名调用。
func historyEntries(entries []sessionentry.ContextEntry) []sessionentry.ContextEntry {
	type callKey struct{ executionID, callID string }
	results := make(map[callKey]sessionentry.ContextEntry)
	calls := make(map[callKey]bool)
	for _, message := range entries {
		if entry, ok := modelVisibleEntry(message); ok {
			for _, block := range entry.Content {
				if block.Kind == sessionentry.BlockToolCall {
					calls[callKey{message.ExecutionID, block.CallID}] = true
				}
				if block.Kind == sessionentry.BlockToolResult {
					result := entry
					result.Content = []sessionentry.Block{block}
					results[callKey{message.ExecutionID, block.CallID}] = result
				}
			}
		}
	}
	selected := make([]sessionentry.ContextEntry, 0, len(entries))
	for _, message := range entries {
		entry, ok := modelVisibleEntry(message)
		if !ok {
			continue
		}
		if message.Role == sessionentry.RoleTool {
			content := make([]sessionentry.Block, 0, len(entry.Content))
			for _, block := range entry.Content {
				if block.Kind != sessionentry.BlockToolResult || !calls[callKey{message.ExecutionID, block.CallID}] {
					content = append(content, block)
				}
			}
			if len(content) == 0 {
				continue
			}
			entry.Content = content
		}
		selected = append(selected, entry)
		for _, block := range entry.Content {
			if block.Kind != sessionentry.BlockToolCall {
				continue
			}
			if result, ok := results[callKey{message.ExecutionID, block.CallID}]; ok {
				selected = append(selected, result)
			}
		}
	}
	return selected
}
