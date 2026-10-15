package service

import (
	"slices"
	"strconv"

	"praxis/internal/contracts"
)

// pageAgentHistory 从最新记录向前分页；游标绑定记录身份，不受后续追加影响。
func pageAgentHistory(items []AgentHistoryItem, before string, limit int) (AgentHistoryPage, error) {
	end := len(items)
	if before != "" {
		end = slices.IndexFunc(items, func(item AgentHistoryItem) bool { return historyItemCursor(item) == before })
		if end < 0 {
			return AgentHistoryPage{}, contracts.ErrNotFound
		}
	}
	start := 0
	if limit > 0 {
		start = max(0, end-limit)
	}
	page := AgentHistoryPage{Items: items[start:end]}
	if start > 0 {
		page.NextCursor = historyItemCursor(items[start])
	}
	return page, nil
}

func historyItemCursor(item AgentHistoryItem) string {
	if item.Message != nil {
		return "message:" + strconv.FormatUint(item.Sequence, 10)
	}
	return item.Kind + ":" + item.Task.ID.String()
}
