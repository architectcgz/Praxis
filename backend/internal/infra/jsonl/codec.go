package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"praxis/internal/fault"
	"strings"
)

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON data")
	}
	return nil
}

func eventKey(scope string, event Event) entityKey {
	if event.Collection == "message" {
		return entityKey{event.Collection, scope + "\x00" + event.Key}
	}
	return entityKey{event.Collection, event.Key}
}

type recordPosition struct {
	scope          string
	offset, length int64
}
type recordReader map[recordPosition]batch

func (s *Store) readRecord(ref recordRef, reader recordReader) (batch, error) {
	position := recordPosition{ref.Scope, ref.Offset, ref.Length}
	if value, ok := reader[position]; ok {
		return value, nil
	}
	path, err := s.path(ref.Scope)
	if err != nil {
		return batch{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return batch{}, err
	}
	if !info.Mode().IsRegular() {
		return batch{}, errors.New("log must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return batch{}, err
	}
	defer file.Close()
	data := make([]byte, ref.Length)
	if _, err := file.ReadAt(data, ref.Offset); err != nil {
		return batch{}, err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return batch{}, errors.New("index points to an uncommitted line")
	}
	var result batch
	if err := decodeJSON(data, &result); err != nil {
		return batch{}, err
	}
	if reader != nil {
		reader[position] = result
	}
	return result, nil
}

func (s *Store) readRef(key entityKey, ref recordRef, reader recordReader) (json.RawMessage, error) {
	record, err := s.readRecord(ref, reader)
	if err != nil {
		return nil, err
	}
	if ref.EventIndex < 0 || ref.EventIndex >= len(record.Events) {
		return nil, errors.New("invalid event index")
	}
	event := record.Events[ref.EventIndex]
	if eventKey(ref.Scope, event) != key || event.Sequence != ref.Sequence || event.ID != fmt.Sprintf("event:%d", ref.Sequence) || event.CreatedAt.IsZero() {
		return nil, errors.New("record identity does not match position index")
	}
	if _, err := restoreIndexEntry(ref.Scope, event); err != nil {
		return nil, err
	}
	return event.Payload, nil
}

func (s *Store) payload(ctx context.Context, key entityKey) (json.RawMessage, error) {
	return s.payloadWithReader(ctx, key, nil)
}

func (s *Store) payloadWithReader(ctx context.Context, key entityKey, reader recordReader) (json.RawMessage, error) {
	if tx := s.tx(ctx); tx != nil {
		if change, ok := tx.changes[key]; ok {
			if tx.deletedScope {
				return nil, fault.ErrNotFound
			}
			return change.event.Payload, nil
		}
	}
	if key.collection == "message" {
		scope, id, ok := strings.Cut(key.id, "\x00")
		if !ok {
			return nil, fault.ErrNotFound
		}
		if tx := s.tx(ctx); tx != nil && tx.deletedScope && tx.scope == scope {
			return nil, fault.ErrNotFound
		}
		if !s.index.hasMessages[scope] {
			return nil, fault.ErrNotFound
		}
		index, err := s.messageIndex(ctx, scope)
		if err != nil {
			return nil, err
		}
		ref, ok := index.byID[id]
		if !ok {
			return nil, fault.ErrNotFound
		}
		return s.readRef(key, ref.location, reader)
	}
	entry, ok := s.entry(ctx, key)
	if !ok {
		return nil, fault.ErrNotFound
	}
	return s.readRef(key, entry.location, reader)
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
		reader := make(recordReader)
		for _, key := range s.keys(readCtx, group) {
			data, err := s.payloadWithReader(readCtx, key, reader)
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
