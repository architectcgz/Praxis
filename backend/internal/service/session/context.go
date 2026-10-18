package session

import (
	"crypto/sha256"
	"encoding/hex"
	"praxis/internal/identity"
	"time"
)

const MaxSessionContextContentBytes = 16 * 1024

// SessionContextKind 标识 Session 共享上下文的事实类别。
type SessionContextKind string

const (
	SessionContextUserMessage        SessionContextKind = "user_message"
	SessionContextAcceptedConclusion SessionContextKind = "accepted_conclusion"
	SessionContextDecision           SessionContextKind = "decision"
	SessionContextReference          SessionContextKind = "reference"
)

// SessionContextEntry 是只追加的 Session 共享事实，不保存消息副本。
type SessionContextEntry struct {
	ID                identity.ContextEntryID
	SessionID         identity.SessionID
	Kind              SessionContextKind
	SourceExecutionID identity.AgentExecutionID
	Content           string
	ContentDigest     string
	CreatedAt         time.Time
}

// NewSessionContextEntry 使用入口已规范化并校验的正文创建共享事实及其摘要。
func NewSessionContextEntry(id identity.ContextEntryID, sessionID identity.SessionID, kind SessionContextKind, sourceExecutionID identity.AgentExecutionID, content string, at time.Time) SessionContextEntry {
	return SessionContextEntry{
		ID:                id,
		SessionID:         sessionID,
		Kind:              kind,
		SourceExecutionID: sourceExecutionID,
		Content:           content,
		ContentDigest:     ContentDigest(content),
		CreatedAt:         at.UTC(),
	}
}

// ContentDigest 返回原始正文的稳定摘要；调用方负责在输入边界完成正文规范化。
func ContentDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return "sha256-" + hex.EncodeToString(digest[:])
}
