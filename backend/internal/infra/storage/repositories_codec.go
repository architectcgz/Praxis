package storage

import (
	"context"
	"encoding/json"
	"errors"
	projectmodel "praxis/internal/core/project"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/fault"
	"praxis/internal/infra/jsonl"
)

func decodeJSON(data []byte, target any) error { return jsonl.Decode(data, target) }

func eventKey(scope string, event jsonl.Event) entityKey {
	if event.Collection == "entry" {
		return entityKey{event.Collection, scope + "\x00" + event.Key}
	}
	return entityKey{event.Collection, event.Key}
}

// payload 及 Locked 查询只供已持有 Store.mu 的代码使用，避免嵌套获取同一把锁。
func (s *Store) payload(key entityKey) (json.RawMessage, error) {
	entry, ok := s.entry(key)
	if !ok {
		if key.collection == "workspace" {
			owner, err := s.lookup(lookupKey{kind: "workspace-id", owner: key.id})
			if err == nil && owner.collection == "project" {
				entry, _ := s.entry(owner)
				var project projectmodel.Project
				if err := decodeJSON(entry.payload, &project); err != nil {
					return nil, err
				}
				return json.Marshal(workspacemodel.NewWorkspace(project.DefaultWorkspaceID, project.ID, workspacemodel.WorkspaceProjectRoot, project.Path, project.CreatedAt))
			}
			if err != nil && !errors.Is(err, fault.ErrNotFound) {
				return nil, err
			}
		}
		return nil, fault.ErrNotFound
	}
	if entry.record != nil {
		return json.Marshal(entry.entryValue())
	}
	return entry.payload, nil
}

func getLocked[T any](s *Store, collection, id string) (T, error) {
	var result T
	data, err := s.payload(entityKey{collection, id})
	if err == nil {
		err = s.decodeProjected(data, &result)
	}
	return result, err
}

func get[T any](ctx context.Context, s *Store, collection, id string) (result T, err error) {
	err = s.view(ctx, func() error {
		result, err = getLocked[T](s, collection, id)
		return err
	})
	return result, err
}

func listIndexedLocked[T any](s *Store, group lookupKey, filter func(T) bool) ([]T, error) {
	result := make([]T, 0)
	for _, key := range s.keys(group) {
		value, err := getLocked[T](s, key.collection, key.id)
		if err != nil {
			return nil, err
		}
		if filter == nil || filter(value) {
			result = append(result, value)
		}
	}
	return result, nil
}

func listIndexed[T any](ctx context.Context, s *Store, group lookupKey, filter func(T) bool) (result []T, err error) {
	err = s.view(ctx, func() error {
		result, err = listIndexedLocked(s, group, filter)
		return err
	})
	return result, err
}

func getByClaimLocked[T any](s *Store, claim lookupKey) (T, error) {
	key, err := s.lookup(claim)
	if err != nil {
		var zero T
		return zero, err
	}
	return getLocked[T](s, key.collection, key.id)
}

func getByClaim[T any](ctx context.Context, s *Store, claim lookupKey) (result T, err error) {
	err = s.view(ctx, func() error {
		result, err = getByClaimLocked[T](s, claim)
		return err
	})
	return result, err
}

func save(ctx context.Context, s *Store, scope, collection, id, kind string, value any) error {
	return s.view(ctx, func() error {
		return s.appendLocked(ctx, scope, collection, id, kind, value)
	})
}
