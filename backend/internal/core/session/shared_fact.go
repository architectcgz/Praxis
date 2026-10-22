package session

import (
	"crypto/sha256"
	"encoding/hex"
	"praxis/internal/identity"
	"time"
)

const MaxSharedFactContentBytes = 16 * 1024

// SharedFactKind 标识 Session 共享上下文的事实类别。
type SharedFactKind string

const (
	SessionContextUserMessage        SharedFactKind = "user_message"
	SessionContextAcceptedConclusion SharedFactKind = "accepted_conclusion"
	SessionContextDecision           SharedFactKind = "decision"
	SessionContextReference          SharedFactKind = "reference"
)

// SharedFact 是只追加的 Session 共享事实，不保存消息副本。
type SharedFact struct {
	ID                identity.ContextEntryID
	SessionID         identity.SessionID
	Kind              SharedFactKind
	SourceExecutionID identity.AgentExecutionID
	Content           string
	ContentDigest     string
	CreatedAt         time.Time
}

// NewSharedFact 使用入口已规范化并校验的正文创建共享事实及其摘要。
func NewSharedFact(id identity.ContextEntryID, sessionID identity.SessionID, kind SharedFactKind, sourceExecutionID identity.AgentExecutionID, content string, at time.Time) SharedFact {
	return SharedFact{
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
