package service

import (
	"errors"
	"testing"
	"time"

	"praxis/internal/contracts"
)

func TestHistoryPagesIncludeLatestAndRemainStableAfterAppend(t *testing.T) {
	items := make([]AgentHistoryItem, 401)
	for i := range items {
		sequence := uint64(i + 1)
		items[i] = AgentHistoryItem{
			Kind:     "message",
			Sequence: sequence,
			At:       time.Unix(int64(i), 0),
			Message:  &AgentMessage{Sequence: sequence},
		}
	}
	latest, err := pageAgentHistory(items, "", 200)
	if err != nil || len(latest.Items) != 200 || latest.Items[0].Sequence != 202 || latest.Items[199].Sequence != 401 {
		t.Fatalf("最新消息未完整返回：%+v, %v", latest, err)
	}
	items = append(items, AgentHistoryItem{Kind: "message", Sequence: 402, Message: &AgentMessage{Sequence: 402}})
	previous, err := pageAgentHistory(items, latest.NextCursor, 200)
	if err != nil || len(previous.Items) != 200 || previous.Items[0].Sequence != 2 || previous.Items[199].Sequence != 201 {
		t.Fatalf("追加消息后续页发生偏移：%+v, %v", previous, err)
	}
	first, err := pageAgentHistory(items, previous.NextCursor, 200)
	if err != nil || len(first.Items) != 1 || first.Items[0].Sequence != 1 || first.NextCursor != "" {
		t.Fatalf("最早消息无法到达：%+v, %v", first, err)
	}
}

func TestHistoryPagesIncludeTaskOnlyRecords(t *testing.T) {
	items := []AgentHistoryItem{
		{Kind: "task", Task: &TaskInfo{ID: "task-1"}},
		{Kind: "request_canceled", Task: &TaskInfo{ID: "task-2"}},
	}
	page, err := pageAgentHistory(items, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Task.ID != "task-2" || page.NextCursor == "" {
		t.Fatalf("取消记录缺失：%+v, %v", page, err)
	}
	previous, err := pageAgentHistory(items, page.NextCursor, 1)
	if err != nil || len(previous.Items) != 1 || previous.Items[0].Task.ID != "task-1" {
		t.Fatalf("仅含失败任务的续页不可读：%+v, %v", previous, err)
	}
	if _, err := pageAgentHistory(items, "message:missing", 1); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("错误游标应拒绝：%v", err)
	}
}
