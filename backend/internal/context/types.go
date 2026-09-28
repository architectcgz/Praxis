// Package context 定义模型可见上下文的独立类型和构建规则。
package context

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// EntryKind 表示共享 SessionContext 条目的业务类别。
type EntryKind string

const (
	EntryUserMessage        EntryKind = "user_message"
	EntryAcceptedConclusion EntryKind = "accepted_conclusion"
	EntryDecision           EntryKind = "decision"
	EntryReference          EntryKind = "reference"
)

// Entry 是 Context 模块自己的共享上下文条目，不暴露领域 model 类型。
type Entry struct {
	ID                string
	SessionID         string
	Revision          uint64
	Kind              EntryKind
	SourceExecutionID string
	Content           string
	ContentDigest     string
	CreatedAt         time.Time
}

// TranscriptMessage 是 ContextBuilder 用于构建 Agent 私有历史的输入投影。
type TranscriptMessage struct {
	Sequence    uint64
	ExecutionID string
	MessageID   string
	Digest      string
	Role        string
	Content     string
	Blocks      []ContextBlock
	InputBytes  int
}

// ContextEntryKind 表示运行期上下文条目的来源。
type ContextEntryKind string

const (
	ContextEntrySession        ContextEntryKind = "session"
	ContextEntryUserInput      ContextEntryKind = "user_input"
	ContextEntryProviderOutput ContextEntryKind = "provider_output"
	ContextEntryToolResult     ContextEntryKind = "tool_result"
	ContextEntryMCPCall        ContextEntryKind = "mcp_call"
	ContextEntryMCPResult      ContextEntryKind = "mcp_result"
)

// ContextRole 表示上下文条目的参与方。
type ContextRole string

const (
	ContextRoleUser      ContextRole = "user"
	ContextRoleAssistant ContextRole = "assistant"
	ContextRoleTool      ContextRole = "tool"
)

// ContextBlockKind 表示上下文条目中的语义内容。
type ContextBlockKind string

const (
	ContextBlockText       ContextBlockKind = "text"
	ContextBlockThinking   ContextBlockKind = "thinking"
	ContextBlockToolCall   ContextBlockKind = "tool_call"
	ContextBlockToolResult ContextBlockKind = "tool_result"
	ContextBlockMCPCall    ContextBlockKind = "mcp_call"
	ContextBlockMCPResult  ContextBlockKind = "mcp_result"
)

// ContextBlock 是 Provider 无关的上下文内容；协议 adapter 负责把它编码成具体格式。
type ContextBlock struct {
	Kind    ContextBlockKind
	Text    string
	CallID  string
	Name    string
	Input   json.RawMessage
	IsError bool
}

// ContextEntry 是运行期上下文中的一个有序语义条目。
type ContextEntry struct {
	Kind    ContextEntryKind
	Role    ContextRole
	Content []ContextBlock
}

// ExecutionContext 是一次 Provider 请求使用的完整模型上下文。
// Session 构建初始内容，runtime 在 loop 中追加 Provider output 和工具结果。
type ExecutionContext struct {
	SystemPrompt string
	Entries      []ContextEntry
}

// Clone 返回 ContextBlock 的独立副本，避免跨边界修改共享输入。
func (b ContextBlock) Clone() ContextBlock {
	b.Input = json.RawMessage(bytes.Clone(b.Input))
	return b
}

// Clone 返回 ContextEntry 的独立副本。
func (e ContextEntry) Clone() ContextEntry {
	content := make([]ContextBlock, len(e.Content))
	for index, block := range e.Content {
		content[index] = block.Clone()
	}
	e.Content = content
	return e
}

// Clone 返回 ExecutionContext 的独立副本。
func (c ExecutionContext) Clone() ExecutionContext {
	entries := make([]ContextEntry, len(c.Entries))
	for index, entry := range c.Entries {
		entries[index] = entry.Clone()
	}
	c.Entries = entries
	return c
}

// Digest 返回当前完整模型上下文的内容摘要。
func (c ExecutionContext) Digest() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256-" + hex.EncodeToString(digest[:]), nil
}

// Validate 校验完整模型上下文的基本结构。
func (c ExecutionContext) Validate() error {
	if len(c.Entries) == 0 {
		return errors.New("execution context requires entries")
	}
	for _, entry := range c.Entries {
		if !validContextEntryKind(entry.Kind) || !validContextRole(entry.Role) || len(entry.Content) == 0 {
			return errors.New("execution context contains an invalid entry")
		}
		for _, block := range entry.Content {
			if err := validateContextBlock(block); err != nil {
				return err
			}
		}
	}
	return nil
}

func validContextEntryKind(kind ContextEntryKind) bool {
	switch kind {
	case ContextEntrySession, ContextEntryUserInput, ContextEntryProviderOutput,
		ContextEntryToolResult, ContextEntryMCPCall, ContextEntryMCPResult:
		return true
	default:
		return false
	}
}

func validContextRole(role ContextRole) bool {
	switch role {
	case ContextRoleUser, ContextRoleAssistant, ContextRoleTool:
		return true
	default:
		return false
	}
}

func validateContextBlock(block ContextBlock) error {
	switch block.Kind {
	case ContextBlockText:
		if strings.TrimSpace(block.Text) == "" {
			return errors.New("execution context contains an empty text block")
		}
	case ContextBlockToolCall, ContextBlockMCPCall:
		if strings.TrimSpace(block.CallID) == "" || strings.TrimSpace(block.Name) == "" {
			return errors.New("execution context contains an invalid call block")
		}
		if len(block.Input) > 0 && !json.Valid(block.Input) {
			return errors.New("execution context contains invalid call input")
		}
	case ContextBlockToolResult, ContextBlockMCPResult:
		if strings.TrimSpace(block.CallID) == "" || strings.TrimSpace(block.Name) == "" {
			return errors.New("execution context contains an invalid result block")
		}
	default:
		return errors.New("execution context contains an unknown block kind")
	}
	return nil
}

// EntryFromTranscriptMessage 把历史消息投影为运行期 Context，思考内容只供展示，不能进入模型请求。
func EntryFromTranscriptMessage(message TranscriptMessage) (ContextEntry, bool) {
	if len(message.Blocks) == 0 {
		return ContextEntry{}, false
	}
	role := ContextRole(message.Role)
	kind := ContextEntryProviderOutput
	switch role {
	case ContextRoleUser:
		kind = ContextEntryUserInput
	case ContextRoleTool:
		kind = ContextEntryToolResult
	case ContextRoleAssistant:
	default:
		return ContextEntry{}, false
	}
	content := make([]ContextBlock, 0, len(message.Blocks))
	for _, block := range message.Blocks {
		if block.Kind != ContextBlockThinking {
			content = append(content, block.Clone())
		}
	}
	if len(content) == 0 {
		return ContextEntry{}, false
	}
	return ContextEntry{Kind: kind, Role: role, Content: content}, true
}

// ValidateEntry 校验共享 SessionContext 条目的边界和顺序数据。
func ValidateEntry(entry Entry, sessionRevision, previousRevision uint64) error {
	if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.SessionID) == "" ||
		entry.Revision == 0 || entry.Revision > sessionRevision || entry.Revision <= previousRevision ||
		strings.TrimSpace(entry.Content) == "" {
		return errors.New("context contains an invalid session entry")
	}
	switch entry.Kind {
	case EntryUserMessage, EntryAcceptedConclusion, EntryDecision, EntryReference:
	default:
		return errors.New("context contains an unknown session entry kind")
	}
	return nil
}
