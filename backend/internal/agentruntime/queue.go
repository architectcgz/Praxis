package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type runtimeQueues struct {
	steer    []QueueItem
	followUp []QueueItem
	nextTurn []QueueItem
	nextID   uint64
}

type queueEntryPayload struct {
	Queue   QueueKind     `json:"queue"`
	ItemID  string        `json:"itemId"`
	Content string        `json:"content,omitempty"`
	Reason  ConsumeReason `json:"reason,omitempty"`
}

// Enqueue durably adds content to a same-agent queue before reporting success.
func (r *Runtime) Enqueue(ctx context.Context, queue QueueKind, content string) (QueueItem, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return QueueItem{}, &RuntimeError{Code: ErrorContract, Message: "queue content is required"}
	}
	if queue != QueueSteer && queue != QueueFollowUp && queue != QueueNextTurn {
		return QueueItem{}, &RuntimeError{Code: ErrorContract, Message: "unknown runtime queue"}
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return QueueItem{}, &RuntimeError{Code: ErrorClosed, Message: "runtime is closed"}
	}
	if queue != QueueNextTurn && r.activeRun == nil {
		r.mu.Unlock()
		return QueueItem{}, &RuntimeError{Code: ErrorBusy, Message: "runtime is not running"}
	}
	r.mu.Unlock()

	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	r.mu.Lock()
	if r.closed || (queue != QueueNextTurn && r.activeRun == nil) {
		r.mu.Unlock()
		return QueueItem{}, &RuntimeError{Code: ErrorBusy, Message: "runtime is not accepting this queue"}
	}
	r.queues.nextID++
	item := QueueItem{ID: fmt.Sprintf("queue-%d", r.queues.nextID), Queue: queue, Content: content}
	r.mu.Unlock()

	payload, err := json.Marshal(queueEntryPayload{Queue: queue, ItemID: item.ID, Content: item.Content})
	if err != nil {
		return QueueItem{}, err
	}
	if err := r.appendEntry(ctx, EntryQueueEnqueued, payload, true); err != nil {
		return QueueItem{}, err
	}
	switch queue {
	case QueueSteer:
		r.queues.steer = append(r.queues.steer, item)
	case QueueFollowUp:
		r.queues.followUp = append(r.queues.followUp, item)
	case QueueNextTurn:
		r.queues.nextTurn = append(r.queues.nextTurn, item)
	}
	return item, nil
}

// Steer queues corrective input for the next save point of the active turn.
func (r *Runtime) Steer(ctx context.Context, content string) (QueueItem, error) {
	return r.Enqueue(ctx, QueueSteer, content)
}

// FollowUp queues input to continue after the current turn would otherwise settle.
func (r *Runtime) FollowUp(ctx context.Context, content string) (QueueItem, error) {
	return r.Enqueue(ctx, QueueFollowUp, content)
}

// NextTurn queues input that survives an abort and is consumed by the next run.
func (r *Runtime) NextTurn(ctx context.Context, content string) (QueueItem, error) {
	return r.Enqueue(ctx, QueueNextTurn, content)
}

// Queued returns defensive copies of the three same-agent queues.
func (r *Runtime) Queued() (steer, followUp, nextTurn []QueueItem) {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	return append([]QueueItem(nil), r.queues.steer...), append([]QueueItem(nil), r.queues.followUp...), append([]QueueItem(nil), r.queues.nextTurn...)
}

func (r *Runtime) drainQueue(ctx context.Context) (bool, error) {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	items := r.queues.steer
	queue := QueueSteer
	if len(items) == 0 {
		items = r.queues.followUp
		queue = QueueFollowUp
	}
	if len(items) == 0 {
		return false, nil
	}
	for _, item := range items {
		if err := r.consumeQueueItem(ctx, item, queue, ConsumeDrained); err != nil {
			return false, err
		}
	}
	if queue == QueueSteer {
		r.queues.steer = nil
	} else {
		r.queues.followUp = nil
	}
	return true, nil
}

func (r *Runtime) clearAbortQueues(ctx context.Context) error {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	var firstErr error
	for _, queued := range []struct {
		queue QueueKind
		items []QueueItem
	}{
		{QueueSteer, r.queues.steer},
		{QueueFollowUp, r.queues.followUp},
	} {
		for _, item := range queued.items {
			if err := r.consumeQueueItem(ctx, item, queued.queue, ConsumeClearedAbort); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	r.queues.steer = nil
	r.queues.followUp = nil
	return firstErr
}

func (r *Runtime) drainNextTurn(ctx context.Context) error {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	for _, item := range r.queues.nextTurn {
		if err := r.consumeQueueItem(ctx, item, QueueNextTurn, ConsumeDrained); err != nil {
			return err
		}
	}
	r.queues.nextTurn = nil
	return nil
}

func (r *Runtime) consumeQueueItem(ctx context.Context, item QueueItem, queue QueueKind, reason ConsumeReason) error {
	payload, err := json.Marshal(queueEntryPayload{Queue: queue, ItemID: item.ID, Reason: reason})
	if err != nil {
		return err
	}
	if err := r.appendEntry(ctx, EntryQueueConsumed, payload, true); err != nil {
		return err
	}
	messagePayload, err := json.Marshal(messagePayload{Role: RoleUser, Content: []ContentBlock{{Kind: ContentText, Text: item.Content}}})
	if err != nil {
		return err
	}
	return r.appendEntry(ctx, EntryMessage, messagePayload, true)
}
