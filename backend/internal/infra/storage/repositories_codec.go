package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"praxis/internal/fault"
	"praxis/internal/infra/jsonl"
)

func decodeJSON(data []byte, target any) error { return jsonl.Decode(data, target) }

func eventKey(scope string, event jsonl.Event) entityKey {
	if event.Collection == "message" {
		return entityKey{event.Collection, scope + "\x00" + event.Key}
	}
	return entityKey{event.Collection, event.Key}
}

func (s *Store) payload(ctx context.Context, key entityKey) (json.RawMessage, error) {
	entry, ok := s.entry(ctx, key)
	if !ok {
		return nil, fault.ErrNotFound
	}
	return entry.payload, nil
}

func get[T any](ctx context.Context, s *Store, collection, id string) (T, error) {
	var result T
	err := s.view(ctx, func(readCtx context.Context) error {
		data, err := s.payload(readCtx, entityKey{collection, id})
		if err != nil {
			return err
		}
		return decodeJSON(data, &result)
	})
	return result, err
}

func listIndexed[T any](ctx context.Context, s *Store, group lookupKey, filter func(T) bool) ([]T, error) {
	result := make([]T, 0)
	err := s.view(ctx, func(readCtx context.Context) error {
		for _, key := range s.keys(readCtx, group) {
			data, err := s.payload(readCtx, key)
			if err != nil {
				return err
			}
			var value T
			if err := decodeJSON(data, &value); err != nil {
				return err
			}
			if filter == nil || filter(value) {
				result = append(result, value)
			}
		}
		return nil
	})
	return result, err
}

func getByClaim[T any](ctx context.Context, s *Store, claim lookupKey) (T, error) {
	var result T
	err := s.view(ctx, func(readCtx context.Context) error {
		key, err := s.lookup(readCtx, claim)
		if err != nil {
			return err
		}
		data, err := s.payload(readCtx, key)
		if err != nil {
			return err
		}
		return decodeJSON(data, &result)
	})
	return result, err
}

func save(ctx context.Context, s *Store, scope, collection, id, kind string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := s.change(ctx, scope, collection, id, kind, data, value); err != nil {
		return fmt.Errorf("%s/%s: %w", collection, id, err)
	}
	return nil
}
