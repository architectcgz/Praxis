package storage

import (
	"encoding/json"
	"maps"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"slices"
)

type entityKey struct{ collection, id string }
type lookupKey struct{ kind, owner, value string }

type stateEntry struct {
	interrupted   bool
	record        *sessionentry.Record
	scope         string
	payload       json.RawMessage
	groups        []lookupKey
	claims        []lookupKey
	eventSequence uint64
}

// state 保存已持久化实体及其可重建索引，只能在 Store.mu 内访问。
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

func (s *Store) keys(group lookupKey) []entityKey {
	return slices.Collect(maps.Keys(s.state.groups[group]))
}

func (s *Store) lookup(claim lookupKey) (entityKey, error) {
	key, ok := s.state.unique[claim]
	if !ok {
		return entityKey{}, errorcode.ErrNotFound
	}
	return key, nil
}

func (s *Store) entry(key entityKey) (stateEntry, bool) {
	entry, ok := s.state.entries[key]
	return entry, ok
}

// entryValue 与 Session History 共享只读正文，索引不再持有另一份序列化正文。
func (e stateEntry) entryValue() entryRecord {
	return entryRecord{OwnerID: e.record.Stream.OwnerAgentID.String(), AgentID: e.record.AgentID, Data: e.record.Data}
}
