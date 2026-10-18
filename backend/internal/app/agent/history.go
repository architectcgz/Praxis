package agent

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	agentcontracts "praxis/internal/app/agent/contracts"
	"praxis/internal/core/fault"
	"praxis/internal/core/identity"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/request"
)

// ListAgentHistory 按稳定时间和身份排列公开消息及失败、取消任务，游标不受后续追加影响。
func (a *App) ListAgentHistory(ctx context.Context, agentID, before string, limit int) (agentcontracts.AgentHistoryPage, error) {
	limit, err := request.ListLimit(limit)
	if err != nil {
		return agentcontracts.AgentHistoryPage{}, err
	}
	messages, err := a.listAgentMessages(ctx, agentID, 0)
	if err != nil {
		return agentcontracts.AgentHistoryPage{}, err
	}
	view, err := a.agents.GetAgentView(ctx, identity.AgentID(agentID), 0)
	if err != nil {
		return agentcontracts.AgentHistoryPage{}, err
	}
	result := make([]agentcontracts.AgentHistoryItem, 0, len(messages)+len(view.Tasks))
	for _, message := range messages {
		result = append(result, agentcontracts.AgentHistoryItem{
			Kind:     "message",
			Sequence: message.Sequence,
			At:       message.At,
			Message:  &message,
		})
	}
	for _, task := range view.Tasks {
		if task.Status != taskmodel.TaskEnded {
			continue
		}
		kind := "task"
		if task.FailureCode == taskmodel.TaskFailureRequestCanceled {
			kind = "request_canceled"
		} else if task.Outcome != taskmodel.TaskFailed {
			continue
		}
		at := task.EndedAt
		if at.IsZero() {
			at = task.CreatedAt
		}
		value := taskInfo(task)
		result = append(result, agentcontracts.AgentHistoryItem{Kind: kind, At: at, Task: &value})
	}
	slices.SortStableFunc(result, func(a, b agentcontracts.AgentHistoryItem) int {
		if order := a.At.Compare(b.At); order != 0 {
			return order
		}
		return cmp.Or(cmp.Compare(a.Sequence, b.Sequence), cmp.Compare(historyItemCursor(a), historyItemCursor(b)))
	})
	return pageAgentHistory(result, before, limit)
}

func pageAgentHistory(items []agentcontracts.AgentHistoryItem, before string, limit int) (agentcontracts.AgentHistoryPage, error) {
	end := len(items)
	if before != "" {
		end = slices.IndexFunc(items, func(item agentcontracts.AgentHistoryItem) bool { return historyItemCursor(item) == before })
		if end < 0 {
			return agentcontracts.AgentHistoryPage{}, fault.ErrNotFound
		}
	}
	start := 0
	if limit > 0 {
		start = max(0, end-limit)
	}
	page := agentcontracts.AgentHistoryPage{Items: items[start:end]}
	if start > 0 {
		page.NextCursor = historyItemCursor(items[start])
	}
	return page, nil
}

func historyItemCursor(item agentcontracts.AgentHistoryItem) string {
	if item.Message != nil {
		return "message:" + strconv.FormatUint(item.Sequence, 10)
	}
	return item.Kind + ":" + item.Task.ID
}
