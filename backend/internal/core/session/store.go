package session

import (
	"context"

	"praxis/internal/core/domain"
)

type SessionEntry struct {
	Kind          string
	TaskSessionID domain.TaskSessionID
	AgentThreadID domain.AgentThreadID
	AgentRunID    domain.AgentRunID
	Sequence      uint64
	Payload       map[string]string
}

type SessionStore interface {
	Append(ctx context.Context, entry SessionEntry) error
	FindArtifact(ctx context.Context, threadID domain.AgentThreadID, injectionKey string) (SessionEntry, error)
	ReadContext(ctx context.Context, threadID domain.AgentThreadID, limit int) ([]SessionEntry, error)
	Repair(ctx context.Context, threadID domain.AgentThreadID) error
	Close(ctx context.Context) error
}
