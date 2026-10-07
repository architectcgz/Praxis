package session

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type AuthorKind string

const (
	AuthorUser   AuthorKind = "user"
	AuthorAgent  AuthorKind = "agent"
	AuthorTool   AuthorKind = "tool"
	AuthorSystem AuthorKind = "system"
)

type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockThinking   BlockKind = "thinking"
	BlockToolCall   BlockKind = "tool_call"
	BlockToolResult BlockKind = "tool_result"
)

type Block struct {
	Kind    BlockKind
	Text    string
	CallID  string
	Name    string
	Input   json.RawMessage `json:",omitempty"`
	IsError bool
}

type MessageData struct {
	ID         string
	Sequence   uint64
	RequestID  string
	HandoffID  string
	TaskID     string
	Role       Role
	AuthorKind AuthorKind
	AuthorID   string
	Blocks     []Block
	CreatedAt  time.Time
}

// Validate 校验消息身份、作者、角色和结构化内容。
func (m MessageData) Validate() error {
	if strings.TrimSpace(m.ID) == "" || m.Sequence == 0 || m.CreatedAt.IsZero() {
		return errors.New("message identity, sequence and creation time are required")
	}
	if !validRole(m.Role) || !validAuthorKind(m.AuthorKind) || len(m.Blocks) == 0 {
		return errors.New("message role, author and blocks are required")
	}
	for _, block := range m.Blocks {
		if err := validateBlock(m.Role, block); err != nil {
			return err
		}
	}
	return nil
}

func validRole(role Role) bool {
	switch role {
	case RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}

func validAuthorKind(kind AuthorKind) bool {
	switch kind {
	case AuthorUser, AuthorAgent, AuthorTool, AuthorSystem:
		return true
	default:
		return false
	}
}

func validateBlock(role Role, block Block) error {
	switch block.Kind {
	case BlockText:
		if strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid text block")
		}
	case BlockThinking:
		if role != RoleAssistant || strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid thinking block")
		}
	case BlockToolCall:
		if role != RoleAssistant || strings.TrimSpace(block.CallID) == "" || strings.TrimSpace(block.Name) == "" || block.IsError ||
			(len(block.Input) > 0 && !json.Valid(block.Input)) {
			return errors.New("message contains an invalid tool call block")
		}
	case BlockToolResult:
		if role != RoleTool || strings.TrimSpace(block.CallID) == "" || strings.TrimSpace(block.Name) == "" || len(block.Input) > 0 {
			return errors.New("message contains an invalid tool result block")
		}
	default:
		return errors.New("message contains an unknown block kind")
	}
	return nil
}

// TextContent 返回消息中的文本块拼接结果；思考、工具调用和工具结果不属于正文。
func (m MessageData) TextContent() string {
	var content strings.Builder
	for _, block := range m.Blocks {
		if block.Kind == BlockText {
			content.WriteString(block.Text)
		}
	}
	return content.String()
}
