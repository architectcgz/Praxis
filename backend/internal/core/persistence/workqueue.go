package persistence

import (
	"context"
	"time"

	"praxis/internal/core/domain"
)

// WorkQueueRepository owns durable independent-task state for one AgentThread.
// Callers that combine queue, run, thread, and lease mutations must invoke
// these methods through the same TxRunner transaction context.
type WorkQueueRepository interface {
	Get(ctx context.Context, id domain.WorkItemID) (domain.QueuedWorkItem, error)
	ListByThread(ctx context.Context, threadID domain.AgentThreadID, status domain.WorkItemStatus, limit int) ([]domain.QueuedWorkItem, error)
	NextSequence(ctx context.Context, threadID domain.AgentThreadID) (uint64, error)
	Enqueue(ctx context.Context, item domain.QueuedWorkItem) error
	Save(ctx context.Context, item domain.QueuedWorkItem) error
	ClaimNext(ctx context.Context, threadID domain.AgentThreadID, runID domain.AgentRunID, at time.Time) (domain.QueuedWorkItem, error)
	CancelQueued(ctx context.Context, id domain.WorkItemID, at time.Time) (domain.QueuedWorkItem, error)
	ReconcileRunning(ctx context.Context, at time.Time) ([]domain.QueuedWorkItem, error)
}
