package storage

import (
	"context"
	"encoding/json"
	"praxis/internal/fault"
	"slices"
)

type entityKey struct{ collection, id string }
type lookupKey struct{ kind, owner, value string }

type stateEntry struct {
	scope         string
	payload       json.RawMessage
	groups        []lookupKey
	claims        []lookupKey
	eventSequence uint64
	orderSequence uint64
}

// state 只保存完整实体和可由事件重建的通用查询索引。
type state struct {
	entries map[entityKey]stateEntry
	groups  map[lookupKey]map[entityKey]struct{}
	unique  map[lookupKey]entityKey
}

func newState() *state {
	return &state{
		entries: make(map[entityKey]stateEntry),
		groups:  make(map[lookupKey]map[entityKey]struct{}),
		unique:  make(map[lookupKey]entityKey),
	}
}

func collectionKey(name string) lookupKey { return lookupKey{kind: "collection", owner: name} }
func scopeKey(scope string) lookupKey     { return lookupKey{kind: "scope", owner: scope} }
func referenceKey(key entityKey) lookupKey {
	return lookupKey{kind: "ref", owner: key.collection, value: key.id}
}

func (s *state) remove(key entityKey) {
	old, ok := s.entries[key]
	if !ok {
		return
	}
	for _, group := range old.groups {
		delete(s.groups[group], key)
		if len(s.groups[group]) == 0 {
			delete(s.groups, group)
		}
	}
	for _, claim := range old.claims {
		if s.unique[claim] == key {
			delete(s.unique, claim)
		}
	}
	delete(s.entries, key)
}

func (s *state) add(key entityKey, entry stateEntry) {
	s.entries[key] = entry
	for _, group := range entry.groups {
		if s.groups[group] == nil {
			s.groups[group] = make(map[entityKey]struct{})
		}
		s.groups[group][key] = struct{}{}
	}
	for _, claim := range entry.claims {
		s.unique[claim] = key
	}
}

func (s *state) removeScope(scope string) {
	for key := range s.groups[scopeKey(scope)] {
		s.remove(key)
	}
}

func (s *Store) keys(ctx context.Context, group lookupKey) []entityKey {
	tx := s.tx(ctx)
	keys := make([]entityKey, 0, len(s.state.groups[group]))
	for key := range s.state.groups[group] {
		if tx != nil {
			if tx.deletedScope && s.state.entries[key].scope == tx.scope {
				continue
			}
			if _, changed := tx.changes[key]; changed {
				continue
			}
		}
		keys = append(keys, key)
	}
	if tx != nil && !tx.deletedScope {
		for key, change := range tx.changes {
			if slices.Contains(change.entry.groups, group) {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func (s *Store) lookup(ctx context.Context, claim lookupKey) (entityKey, error) {
	if tx := s.tx(ctx); tx != nil {
		if !tx.deletedScope {
			for key, change := range tx.changes {
				if slices.Contains(change.entry.claims, claim) {
					return key, nil
				}
			}
		}
		if key, ok := s.state.unique[claim]; ok {
			if _, changed := tx.changes[key]; changed {
				return entityKey{}, fault.ErrNotFound
			}
			if tx.deletedScope && s.state.entries[key].scope == tx.scope {
				return entityKey{}, fault.ErrNotFound
			}
		}
	}
	key, ok := s.state.unique[claim]
	if !ok {
		return entityKey{}, fault.ErrNotFound
	}
	return key, nil
}

func (s *Store) entry(ctx context.Context, key entityKey) (stateEntry, bool) {
	if tx := s.tx(ctx); tx != nil {
		if changed, ok := tx.changes[key]; ok {
			if tx.deletedScope {
				return stateEntry{}, false
			}
			return changed.entry, true
		}
		if tx.deletedScope && s.state.entries[key].scope == tx.scope {
			return stateEntry{}, false
		}
	}
	entry, ok := s.state.entries[key]
	return entry, ok
}

func (s *Store) publish(tx *transaction) {
	if tx.deletedScope {
		s.state.removeScope(tx.scope)
		return
	}
	// 先移除所有旧索引，再发布最终实体，允许事务内合法的唯一键交换。
	for key := range tx.changes {
		s.state.remove(key)
	}
	for key, change := range tx.changes {
		s.state.add(key, change.entry)
	}
}
