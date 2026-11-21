package entry

import (
	"fmt"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"time"
)

const interruptedToolResultText = "The execution ended before a reliable tool result was available."

// Key 复用工具契约的调用身份。
type Key = tool.CallKey

// ToolCall 是工具调用及执行归属的内存表示，调用信息随 Assistant Entry 持久化。
type ToolCall struct {
	Key
	SessionID identity.SessionID
	AgentID   identity.AgentID
	TurnID    identity.TurnID
	Name      tool.ToolName
	Arguments string
}

// MissingToolResultEntries 按 (RunID, CallID) 差集为未闭合的工具调用生成错误回执条目。
// 保留原声明顺序，不覆盖已有回执；用于正常中断收尾与异常关闭恢复共用同一事实语义。
func MissingToolResultEntries(entries []*Entry, createdAt time.Time) []Entry {
	type callKey struct {
		runID  string
		callID string
	}
	completed := make(map[callKey]bool)
	usedIDs := make(map[string]bool, len(entries))
	for _, value := range entries {
		if value.ID != "" {
			usedIDs[value.ID] = true
		}
		_, blocks := value.Dialogue()
		for _, block := range blocks {
			if result, ok := block.(ToolResultBlock); ok {
				completed[callKey{runID: value.RunID, callID: result.CallID}] = true
			}
		}
	}
	var missing []Entry
	for _, value := range entries {
		var results Content
		_, blocks := value.Dialogue()
		for _, block := range blocks {
			call, ok := block.(ToolCallBlock)
			if !ok {
				continue
			}
			key := callKey{runID: value.RunID, callID: call.CallID}
			if completed[key] {
				continue
			}
			completed[key] = true
			results = append(results, ToolResultBlock{
				CallID:  call.CallID,
				Name:    call.Name,
				Text:    interruptedToolResultText,
				IsError: true,
			})
		}
		if len(results) == 0 {
			continue
		}
		id := nextToolResultEntryID(value, usedIDs)
		usedIDs[id] = true
		missing = append(missing, Entry{
			ID:         id,
			RunID:      value.RunID,
			TurnID:     value.TurnID,
			AuthorKind: AuthorTool,
			CreatedAt:  createdAt,
			Payload: Message{
				Role:    RoleTool,
				Content: results,
			},
		})
	}
	return missing
}

func nextToolResultEntryID(source *Entry, used map[string]bool) string {
	base := "tools:" + source.TurnID
	if source.TurnID == "" {
		base = "tools:" + source.ID
	}
	if !used[base] {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s:%d", base, suffix)
		if !used[candidate] {
			return candidate
		}
	}
}
