package storage

import (
	"context"
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/infra/jsonl"
	"slices"
)

func decodeJSON(data []byte, target any) error { return jsonl.Decode(data, target) }

// logRecord 保存已解码的业务值；event 仅保留元信息，不保留编码正文。
type logRecord struct {
	scope string
	event jsonl.Event
	value any
}

func findRecord(records []logRecord, collection, id string) (logRecord, error) {
	for _, record := range records {
		if record.event.Collection == collection && record.event.Key == id {
			return record, nil
		}
	}
	return logRecord{}, errorcode.ErrNotFound
}

// cloneRecordValue 隔离仓储返回值中的切片与指针，查询方不能修改存储持有的状态。
func cloneRecordValue(value any) any {
	switch v := value.(type) {
	case projectmodel.Project:
		if v.ApprovalMode != nil {
			v.ApprovalMode = new(*v.ApprovalMode)
		}
		return v
	case agentexecution.Execution:
		v.Config = v.Config.Clone()
		return v
	case entryRecord:
		v.Data = v.Data.Clone()
		return v
	case providerapi.UsageRecord:
		if v.Usage.OutputTokens != nil {
			v.Usage.OutputTokens = new(*v.Usage.OutputTokens)
		}
		if v.Usage.CacheReadInputTokens != nil {
			v.Usage.CacheReadInputTokens = new(*v.Usage.CacheReadInputTokens)
		}
		if v.Usage.CacheCreationInputTokens != nil {
			v.Usage.CacheCreationInputTokens = new(*v.Usage.CacheCreationInputTokens)
		}
		return v
	case timing.Record:
		if v.DurationMS != nil {
			v.DurationMS = new(*v.DurationMS)
		}
		if v.FirstResponseMS != nil {
			v.FirstResponseMS = new(*v.FirstResponseMS)
		}
		return v
	case workflow.WaitCondition:
		v.TargetIDs = slices.Clone(v.TargetIDs)
		v.ResolvedTargetIDs = slices.Clone(v.ResolvedTargetIDs)
		return v
	default:
		return value
	}
}

func getRecord[T any](records []logRecord, collection, id string) (result T, err error) {
	record, err := findRecord(records, collection, id)
	if errors.Is(err, errorcode.ErrNotFound) && collection == "workspace" {
		projects, readErr := listRecords[projectmodel.Project](records, "project", nil)
		if readErr != nil {
			return result, readErr
		}
		for _, project := range projects {
			if project.DefaultWorkspaceID.String() == id {
				value := workspacemodel.NewWorkspace(project.DefaultWorkspaceID, project.ID, workspacemodel.WorkspaceProjectRoot, project.Path, project.CreatedAt)
				result, ok := any(value).(T)
				if !ok {
					return result, fmt.Errorf("unexpected value type for %s", collection)
				}
				return result, nil
			}
		}
	}
	if err != nil {
		return result, err
	}
	result, ok := cloneRecordValue(record.value).(T)
	if !ok {
		return result, fmt.Errorf("unexpected value type for %s", collection)
	}
	if err := projectRecord(records, &result); err != nil {
		return result, err
	}
	return result, nil
}

func get[T any](ctx context.Context, storage *Storage, collection, id string) (result T, err error) {
	err = withState(ctx, storage, func(_ *Storage, records []logRecord) error {
		result, err = getRecord[T](records, collection, id)
		return err
	})
	return result, err
}

func listRecords[T any](records []logRecord, collection string, filter func(T) bool) ([]T, error) {
	result := make([]T, 0)
	for _, record := range records {
		if record.event.Collection != collection {
			continue
		}
		value, ok := cloneRecordValue(record.value).(T)
		if !ok {
			return nil, fmt.Errorf("unexpected value type for %s", collection)
		}
		if err := projectRecord(records, &value); err != nil {
			return nil, err
		}
		if filter == nil || filter(value) {
			result = append(result, value)
		}
	}
	return result, nil
}

func list[T any](ctx context.Context, storage *Storage, collection string, filter func(T) bool) (result []T, err error) {
	err = withState(ctx, storage, func(_ *Storage, records []logRecord) error {
		result, err = listRecords(records, collection, filter)
		return err
	})
	return result, err
}

func save(ctx context.Context, storage *Storage, scope, collection, id, kind string, value any) error {
	return withState(ctx, storage, func(state *Storage, records []logRecord) error {
		return appendRecord(state, records, scope, collection, id, kind, value)
	})
}
