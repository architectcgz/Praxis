package bindings

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	taskmodel "praxis/internal/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/wails/dto"
)

// listAgentHistory 在协议出口合并可展示消息与失败、取消事实，游标不受追加影响。
func listAgentHistory(ctx context.Context, agents AgentService, agentID identity.AgentID, before string, limit int) (dto.AgentHistoryPage, error) {
	messages, err := agents.ListAgentMessages(ctx, agentID, 0)
	if err != nil {
		return dto.AgentHistoryPage{}, err
	}
	view, err := agents.GetAgentView(ctx, agentID, 0)
	if err != nil {
		return dto.AgentHistoryPage{}, err
	}
	items := make([]dto.AgentHistoryItem, 0, len(messages)+len(view.Tasks))
	for _, value := range publicAgentMessages(messages) {
		items = append(items, dto.AgentHistoryItem{
			Kind:     "message",
			Sequence: value.Sequence,
			At:       value.At,
			Message:  &value,
		})
	}
	for _, value := range view.Tasks {
		if value.Status != taskmodel.ExecutionEnded {
			continue
		}
		kind := "task"
		if value.FailureCode == taskmodel.ExecutionFailureRequestCanceled {
			kind = "request_canceled"
		} else if value.Outcome != taskmodel.ExecutionFailed {
			continue
		}
		at := value.EndedAt
		if at.IsZero() {
			at = value.CreatedAt
		}
		snapshot := taskSnapshot(value)
		items = append(items, dto.AgentHistoryItem{Kind: kind, At: at, Task: &snapshot})
	}
	slices.SortStableFunc(items, func(a, b dto.AgentHistoryItem) int {
		if order := a.At.Compare(b.At); order != 0 {
			return order
		}
		return cmp.Or(cmp.Compare(a.Sequence, b.Sequence), cmp.Compare(historyItemCursor(a), historyItemCursor(b)))
	})
	end := len(items)
	if before != "" {
		end = slices.IndexFunc(items, func(value dto.AgentHistoryItem) bool { return historyItemCursor(value) == before })
		if end < 0 {
			return dto.AgentHistoryPage{}, fault.ErrNotFound
		}
	}
	start := max(0, end-limit)
	page := dto.AgentHistoryPage{Items: items[start:end]}
	if start > 0 {
		page.NextCursor = historyItemCursor(items[start])
	}
	return page, nil
}

func historyItemCursor(value dto.AgentHistoryItem) string {
	if value.Message != nil {
		return "message:" + strconv.FormatUint(value.Sequence, 10)
	}
	return value.Kind + ":" + value.Task.ID
}
