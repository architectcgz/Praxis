package task

import (
	"bytes"
	"errors"
	"fmt"
	contextmodel "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	"slices"
	"strings"
)

// contextContent 将已选业务数据转换为通用有序条目；当前输入和共享事实保留原文。
func contextContent(systemPrompt, inputMessageID string, shared []sessionmodel.SessionContextEntry, messages, continuation []sessionmodel.MessageData) (contextmodel.Content, error) {
	content := contextmodel.Content{SystemPrompt: systemPrompt}
	shared = slices.Clone(shared)
	slices.SortFunc(shared, func(a, b sessionmodel.SessionContextEntry) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	for _, entry := range shared {
		content.Entries = append(content.Entries, contextmodel.ContextEntry{
			Kind:      contextmodel.ContextEntryReference,
			Role:      contextmodel.ContextRoleUser,
			Protected: true,
			Content:   []contextmodel.ContextBlock{{Kind: contextmodel.ContextBlockText, Text: fmt.Sprintf("[Session context: %s]\n%s", entry.Kind, entry.Content)}},
		})
	}
	var current sessionmodel.MessageData
	history := make([]sessionmodel.MessageData, 0, len(messages))
	for _, message := range messages {
		if message.ID == inputMessageID {
			current = message
		} else {
			history = append(history, message)
		}
	}
	if current.ID == "" || current.Role != sessionmodel.RoleUser {
		return contextmodel.Content{}, errors.New("current input user message is missing")
	}
	entry, ok := entryFromMessageData(current)
	if !ok {
		return contextmodel.Content{}, errors.New("current input has no model-visible content")
	}
	entry.Protected = true
	content.Entries = append(content.Entries, historyEntries(history)...)
	content.Entries = append(content.Entries, entry)
	content.Entries = append(content.Entries, historyEntries(continuation)...)
	return content, nil
}

// entryFromMessageData 把消息投影为运行期 Context，思考内容只供展示，不能进入模型请求。
func entryFromMessageData(value sessionmodel.MessageData) (contextmodel.ContextEntry, bool) {
	if len(value.Blocks) == 0 {
		return contextmodel.ContextEntry{}, false
	}
	role := contextmodel.ContextRole(value.Role)
	kind := contextmodel.ContextEntryProviderOutput
	switch role {
	case contextmodel.ContextRoleUser:
		kind = contextmodel.ContextEntryUserInput
	case contextmodel.ContextRoleTool:
		kind = contextmodel.ContextEntryToolResult
	case contextmodel.ContextRoleAssistant:
	default:
		return contextmodel.ContextEntry{}, false
	}
	content := make([]contextmodel.ContextBlock, 0, len(value.Blocks))
	for _, block := range value.Blocks {
		if block.Kind != sessionmodel.BlockThinking {
			content = append(content, contextmodel.ContextBlock{
				Kind:           contextmodel.ContextBlockKind(block.Kind),
				Text:           block.Text,
				CallID:         block.CallID,
				Name:           block.Name,
				Input:          bytes.Clone(block.Input),
				IsError:        block.IsError,
				ReasoningState: block.ReasoningState.Clone(),
			})
		}
	}
	if len(content) == 0 {
		return contextmodel.ContextEntry{}, false
	}
	return contextmodel.ContextEntry{Kind: kind, Role: role, Content: content}, true
}

// historyEntries 把回执关联到对应调用后，恢复补写和执行期间入队的用户消息不打断工具交互。
// 只调整模型投影，不改写日志顺序；使用 Task 和 call ID 避免跨任务误配同名调用。
func historyEntries(messages []sessionmodel.MessageData) []contextmodel.ContextEntry {
	type callKey struct{ taskID, callID string }
	results := make(map[callKey]contextmodel.ContextBlock)
	calls := make(map[callKey]bool)
	for _, message := range messages {
		if entry, ok := entryFromMessageData(message); ok {
			for _, block := range entry.Content {
				if block.Kind == contextmodel.ContextBlockToolCall {
					calls[callKey{message.TaskID, block.CallID}] = true
				}
				if block.Kind == contextmodel.ContextBlockToolResult {
					results[callKey{message.TaskID, block.CallID}] = block
				}
			}
		}
	}
	entries := make([]contextmodel.ContextEntry, 0, len(messages))
	for _, message := range messages {
		entry, ok := entryFromMessageData(message)
		if !ok {
			continue
		}
		if message.Role == sessionmodel.RoleTool {
			content := make([]contextmodel.ContextBlock, 0, len(entry.Content))
			for _, block := range entry.Content {
				if block.Kind != contextmodel.ContextBlockToolResult || !calls[callKey{message.TaskID, block.CallID}] {
					content = append(content, block)
				}
			}
			if len(content) == 0 {
				continue
			}
			entry.Content = content
		}
		entries = append(entries, entry)
		for _, block := range entry.Content {
			if block.Kind != contextmodel.ContextBlockToolCall {
				continue
			}
			if result, ok := results[callKey{message.TaskID, block.CallID}]; ok {
				entries = append(entries, contextmodel.ContextEntry{
					Kind:    contextmodel.ContextEntryToolResult,
					Role:    contextmodel.ContextRoleTool,
					Content: []contextmodel.ContextBlock{result},
				})
			}
		}
	}
	return entries
}
