// Package timing 提供独立操作计时的公开查询和快照投影。
package timing

import (
	"context"
	"errors"

	timingcontracts "praxis/internal/app/timing/contracts"
	"praxis/internal/request"
	"praxis/internal/timing"
)

// App 只查询计时事实，不读取或修改消息正文。
type App struct {
	listAgent func(context.Context, string, int) ([]timing.Record, error)
}

// New 注入只读计时能力，不依赖具体存储实现。
func New(listAgent func(context.Context, string, int) ([]timing.Record, error)) *App {
	return &App{listAgent: listAgent}
}

// ListAgent 返回 Agent 最近的计时；负数分页返回输入错误。
func (a *App) ListAgent(ctx context.Context, agentID string, limit int) ([]timingcontracts.OperationTiming, error) {
	limit, err := request.ListLimit(limit)
	if err != nil {
		return nil, err
	}
	if a.listAgent == nil {
		return nil, errors.New("timing query is unavailable")
	}
	records, err := a.listAgent(ctx, agentID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]timingcontracts.OperationTiming, 0, len(records))
	for _, record := range records {
		result = append(result, PublicTiming(record))
	}
	return result, nil
}

// PublicTiming 复制计时事实，未知耗时不能补零或与事件观察者共享指针。
func PublicTiming(record timing.Record) timingcontracts.OperationTiming {
	return timingcontracts.OperationTiming{
		SessionID:       record.SessionID,
		AgentID:         record.AgentID,
		TaskID:          record.TaskID,
		Kind:            string(record.Kind),
		Name:            record.Name,
		ReferenceID:     record.ReferenceID,
		ID:              record.ID,
		ParentID:        record.ParentID,
		StartedAt:       record.StartedAt,
		FinishedAt:      record.FinishedAt,
		DurationMS:      clonePointer(record.DurationMS),
		FirstResponseMS: clonePointer(record.FirstResponseMS),
		Status:          string(record.Status),
		Revision:        record.Revision,
	}
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
