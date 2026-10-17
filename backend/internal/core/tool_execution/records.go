package toolexecution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/core/session"
	"praxis/internal/utils/pathutil"
)

type RecordType string

const (
	RecordMessage           RecordType = "message"
	RecordReserved          RecordType = "tool.reserved"
	RecordPrepared          RecordType = "tool.prepared"
	RecordApprovalRequested RecordType = "tool.approval_requested"
	RecordApproved          RecordType = "tool.approved"
	RecordStarted           RecordType = "tool.started"
)

type Status string

const (
	StatusNotStarted       Status = "not_started"
	StatusPrepared         Status = "prepared"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusApproved         Status = "approved"
	StatusSucceeded        Status = "succeeded"
	StatusFailed           Status = "failed"
	StatusDenied           Status = "denied"
	StatusInterrupted      Status = "interrupted"
	StatusUnknown          Status = "unknown"
)

// CallKey 固定工具调用归属；相同 CallID 在不同 Task 中是不同调用。
type CallKey struct {
	SessionID contracts.SessionID `json:"session_id"`
	AgentID   contracts.AgentID   `json:"agent_id"`
	TaskID    contracts.TaskID    `json:"task_id"`
	CallID    string              `json:"call_id"`
}

// Validate 拒绝空身份及非 canonical 身份，不静默修正恢复数据。
func (k CallKey) Validate() error {
	for _, value := range []string{k.SessionID.String(), k.AgentID.String(), k.TaskID.String(), k.CallID} {
		if !canonicalID(value) {
			return errors.New("tool call ownership must contain canonical non-empty identities")
		}
	}
	return nil
}

type Preparation struct {
	NormalizedArguments json.RawMessage `json:"normalized_arguments"`
	Path                string          `json:"path,omitempty"`
}

// Validate 校验冻结参数和授权路径；具体工具参数只能由工具边界规范化。
func (p Preparation) Validate() error {
	if len(p.NormalizedArguments) == 0 || !json.Valid(p.NormalizedArguments) {
		return errors.New("prepared tool arguments must contain valid JSON")
	}
	if p.Path != "" && !pathutil.IsAbsoluteNormalized(p.Path) {
		return errors.New("prepared tool path must be canonical and absolute")
	}
	return nil
}

// Outcome 随 ToolResult 消息保存，不再追加独立的 completed 事件。
type Outcome struct {
	Status     Status `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	SideEffect bool   `json:"side_effect,omitzero"`
	Truncated  bool   `json:"truncated,omitzero"`
}

// Validate 校验终态与错误码的一致性；unknown 同样是不可自动重试的结果。
func (o Outcome) Validate() error {
	switch o.Status {
	case StatusSucceeded:
		if o.ErrorCode != "" {
			return errors.New("successful tool result cannot contain an error code")
		}
	case StatusFailed, StatusDenied, StatusInterrupted, StatusUnknown:
		if !canonicalID(o.ErrorCode) {
			return errors.New("unsuccessful tool result requires a canonical error code")
		}
	default:
		return errors.New("tool result requires a terminal status")
	}
	return nil
}

// Record 统一表示消息与工具执行记录，各类记录只允许携带对应的正文。
type Record struct {
	Sequence  uint64                    `json:"seq"`
	ID        string                    `json:"event_id"`
	Type      RecordType                `json:"type"`
	SessionID contracts.SessionID       `json:"session_id"`
	AgentID   contracts.AgentID         `json:"agent_id"`
	TaskID    contracts.TaskID          `json:"task_id,omitempty"`
	CallID    string                    `json:"call_id,omitempty"`
	CreatedAt time.Time                 `json:"created_at"`
	Message   *session.MessageData      `json:"message,omitempty"`
	Prepared  *Preparation              `json:"prepared,omitempty"`
	Approval  *contracts.ApprovalRecord `json:"approval,omitempty"`
	Outcome   *Outcome                  `json:"outcome,omitempty"`
}

// Key 返回工具执行记录或 ToolResult 的完整调用身份。
func (e Record) Key() CallKey {
	return CallKey{SessionID: e.SessionID, AgentID: e.AgentID, TaskID: e.TaskID, CallID: e.CallID}
}

// Validate 只读校验记录形状、消息归属及序号；状态转换由 ApplyRecord 校验。
func (e Record) Validate() error {
	if e.Sequence == 0 || e.ID != fmt.Sprintf("event:%d", e.Sequence) || e.CreatedAt.IsZero() ||
		!canonicalID(e.SessionID.String()) || !canonicalID(e.AgentID.String()) ||
		e.TaskID != "" && !canonicalID(e.TaskID.String()) {
		return errors.New("invalid tool record identity, sequence or timestamp")
	}
	if e.Type == RecordMessage {
		if e.Message == nil || e.Prepared != nil || e.Approval != nil {
			return errors.New("message event has invalid payload fields")
		}
		m := e.Message
		if err := m.Validate(); err != nil {
			return err
		}
		for _, value := range []string{m.ID, m.RequestID, m.HandoffID, m.TaskID, m.TurnID, m.AuthorID} {
			if value != "" && !canonicalID(value) {
				return errors.New("message identity fields must be canonical")
			}
		}
		if !canonicalID(m.ID) || m.Sequence != e.Sequence || m.TaskID != e.TaskID.String() {
			return errors.New("message identity does not match its event")
		}
		if m.Role == session.RoleAssistant && (m.AuthorKind != session.AuthorAgent || m.AuthorID != e.AgentID.String()) {
			return errors.New("assistant author does not match the event agent")
		}
		if m.Role != session.RoleTool {
			if e.CallID != "" || e.Outcome != nil {
				return errors.New("only tool result messages may contain an outcome or call identity")
			}
			return nil
		}
		if err := e.Key().Validate(); err != nil {
			return err
		}
		if e.Outcome == nil || m.AuthorKind != session.AuthorTool || len(m.Blocks) != 1 ||
			m.Blocks[0].Kind != session.BlockToolResult || m.Blocks[0].CallID != e.CallID ||
			m.Blocks[0].IsError != (e.Outcome.Status != StatusSucceeded) {
			return errors.New("tool result message and outcome are inconsistent")
		}
		if len(m.Blocks[0].Text) > 50*1024 {
			return errors.New("tool result exceeds the inline size limit")
		}
		return e.Outcome.Validate()
	}
	if err := e.Key().Validate(); err != nil {
		return err
	}
	if e.Message != nil || e.Outcome != nil {
		return errors.New("tool lifecycle record cannot contain a message or outcome")
	}
	switch e.Type {
	case RecordPrepared:
		if e.Prepared == nil || e.Approval != nil {
			return errors.New("prepared event requires only prepared arguments")
		}
		return e.Prepared.Validate()
	case RecordApproved:
		if e.Approval == nil || e.Prepared != nil {
			return errors.New("approved event requires only an approval record")
		}
		return e.Approval.Validate()
	case RecordReserved, RecordApprovalRequested, RecordStarted:
		if e.Prepared != nil || e.Approval != nil {
			return errors.New("tool lifecycle record contains unexpected payload fields")
		}
		return nil
	default:
		return errors.New("unknown tool record type")
	}
}

// State 根据消息与工具执行记录重建，不允许作为独立实体持久化。
type State struct {
	Key              CallKey
	Name             contracts.ToolName
	TurnID           contracts.TurnID
	Arguments        json.RawMessage
	Reserved         bool
	Prepared         *Preparation
	Approval         *contracts.ApprovalRecord
	Status           Status
	StartedAt        time.Time
	Outcome          *Outcome
	ResultMessageID  string
	Sequence         uint64
	DeclaredSequence uint64
	CallIndex        int
	OutputBytes      int64
}

// ArgumentsDigest 绑定工具名与冻结参数；摘要不能替代参数快照。
func ArgumentsDigest(name contracts.ToolName, arguments json.RawMessage) string {
	encoded, err := json.Marshal(struct {
		Name      contracts.ToolName
		Arguments json.RawMessage
	}{Name: name, Arguments: arguments})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func canonicalID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}
