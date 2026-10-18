package input

import (
	"bytes"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
	sessionmodel "praxis/internal/service/session"
	"slices"
	"strings"
)

// contextContent 将已选业务数据转换为通用有序条目；当前输入和共享事实保留原文。
func contextContent(systemPrompt, inputMessageID string, shared []sessionmodel.SessionContextEntry, messages, continuation []messagemodel.Message) (appcontext.Content, error) {
	content := appcontext.Content{SystemPrompt: systemPrompt}
	shared = slices.Clone(shared)
	slices.SortFunc(shared, func(a, b sessionmodel.SessionContextEntry) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	for _, entry := range shared {
		content.Entries = append(content.Entries, appcontext.ContextEntry{
			Kind:      appcontext.ContextEntryReference,
			Role:      appcontext.ContextRoleUser,
			Protected: true,
			Content:   []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: fmt.Sprintf("[Session context: %s]\n%s", entry.Kind, entry.Content)}},
		})
	}
	var current messagemodel.Message
	history := make([]messagemodel.Message, 0, len(messages))
	for _, message := range messages {
		if message.ID == inputMessageID {
			current = message
		} else {
			history = append(history, message)
		}
	}
	if current.ID == "" || current.Role != messagemodel.RoleUser {
		return appcontext.Content{}, errors.New("current input user message is missing")
	}
	entry, ok := entryFromMessageData(current)
	if !ok {
		return appcontext.Content{}, errors.New("current input has no model-visible content")
	}
	entry.Protected = true
	content.Entries = append(content.Entries, historyEntries(history)...)
	content.Entries = append(content.Entries, entry)
	content.Entries = append(content.Entries, historyEntries(continuation)...)
	return content, nil
}

// entryFromMessageData 把消息投影为运行期 Context，思考内容只供展示，不能进入模型请求。
func entryFromMessageData(value messagemodel.Message) (appcontext.ContextEntry, bool) {
	if len(value.Blocks) == 0 {
		return appcontext.ContextEntry{}, false
	}
	role := appcontext.ContextRole(value.Role)
	kind := appcontext.ContextEntryProviderOutput
	switch role {
	case appcontext.ContextRoleUser:
		kind = appcontext.ContextEntryUserInput
	case appcontext.ContextRoleTool:
		kind = appcontext.ContextEntryToolResult
	case appcontext.ContextRoleAssistant:
	default:
		return appcontext.ContextEntry{}, false
	}
	content := make([]appcontext.ContextBlock, 0, len(value.Blocks))
	for _, block := range value.Blocks {
		if block.Kind != messagemodel.BlockThinking {
			content = append(content, appcontext.ContextBlock{
				Kind:           appcontext.ContextBlockKind(block.Kind),
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
		return appcontext.ContextEntry{}, false
	}
	return appcontext.ContextEntry{Kind: kind, Role: role, Content: content}, true
}

// historyEntries 把回执关联到对应调用后，恢复补写和执行期间入队的用户消息不打断工具交互。
// 只调整模型投影，不改写日志顺序；使用 Execution 和 call ID 避免跨任务误配同名调用。
func historyEntries(messages []messagemodel.Message) []appcontext.ContextEntry {
	type callKey struct{ executionID, callID string }
	results := make(map[callKey]appcontext.ContextBlock)
	calls := make(map[callKey]bool)
	for _, message := range messages {
		if entry, ok := entryFromMessageData(message); ok {
			for _, block := range entry.Content {
				if block.Kind == appcontext.ContextBlockToolCall {
					calls[callKey{message.ExecutionID, block.CallID}] = true
				}
				if block.Kind == appcontext.ContextBlockToolResult {
					results[callKey{message.ExecutionID, block.CallID}] = block
				}
			}
		}
	}
	entries := make([]appcontext.ContextEntry, 0, len(messages))
	for _, message := range messages {
		entry, ok := entryFromMessageData(message)
		if !ok {
			continue
		}
		if message.Role == messagemodel.RoleTool {
			content := make([]appcontext.ContextBlock, 0, len(entry.Content))
			for _, block := range entry.Content {
				if block.Kind != appcontext.ContextBlockToolResult || !calls[callKey{message.ExecutionID, block.CallID}] {
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
			if block.Kind != appcontext.ContextBlockToolCall {
				continue
			}
			if result, ok := results[callKey{message.ExecutionID, block.CallID}]; ok {
				entries = append(entries, appcontext.ContextEntry{
					Kind:    appcontext.ContextEntryToolResult,
					Role:    appcontext.ContextRoleTool,
					Content: []appcontext.ContextBlock{result},
				})
			}
		}
	}
	return entries
}
