package jsonl

import (
	"context"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"slices"
)

type entityKey struct{ collection, id string }
type lookupKey struct{ kind, owner, value string }

type recordRef struct {
	Scope      string
	Offset     int64
	Length     int64
	EventIndex int
	Sequence   uint64
}

// 只保存位置、查询键及队列序号，不保存领域对象或 JSON 正文。
type indexEntry struct {
	location recordRef
	groups   []lookupKey
	claims   []lookupKey
	sequence uint64
}

type index struct {
	latest      map[entityKey]indexEntry
	groups      map[lookupKey]map[entityKey]struct{}
	unique      map[lookupKey]entityKey
	maxQueue    map[string]uint64
	hasMessages map[string]bool
	deleted     map[string]bool
	missing     map[receiptKey]messagemodel.MissingToolResult
}

func newIndex() *index {
	return &index{
		latest:      make(map[entityKey]indexEntry),
		groups:      make(map[lookupKey]map[entityKey]struct{}),
		unique:      make(map[lookupKey]entityKey),
		maxQueue:    make(map[string]uint64),
		hasMessages: make(map[string]bool),
		deleted:     make(map[string]bool),
		missing:     make(map[receiptKey]messagemodel.MissingToolResult),
	}
}

func collectionKey(name string) lookupKey { return lookupKey{kind: "collection", owner: name} }
func scopeKey(scope string) lookupKey     { return lookupKey{kind: "scope", owner: scope} }
func referenceKey(key entityKey) lookupKey {
	return lookupKey{kind: "ref", owner: key.collection, value: key.id}
}

func (i *index) remove(key entityKey) {
	old, ok := i.latest[key]
	if !ok {
		return
	}
	for _, group := range old.groups {
		delete(i.groups[group], key)
		if len(i.groups[group]) == 0 {
			delete(i.groups, group)
		}
	}
	for _, claim := range old.claims {
		if i.unique[claim] == key {
			delete(i.unique, claim)
		}
	}
	delete(i.latest, key)
}

func (i *index) add(key entityKey, entry indexEntry) {
	i.latest[key] = entry
	for _, group := range entry.groups {
		if i.groups[group] == nil {
			i.groups[group] = make(map[entityKey]struct{})
		}
		i.groups[group][key] = struct{}{}
	}
	for _, claim := range entry.claims {
		i.unique[claim] = key
	}
	if key.collection == "task" {
		for _, group := range entry.groups {
			if group.kind == "tasks-agent" {
				i.maxQueue[group.owner] = max(i.maxQueue[group.owner], entry.sequence)
			}
		}
	}
}

func (s *Store) keys(ctx context.Context, group lookupKey) []entityKey {
	tx := s.tx(ctx)
	keys := make([]entityKey, 0, len(s.index.groups[group]))
	for key := range s.index.groups[group] {
		if tx != nil {
			if tx.deletedScope && s.index.latest[key].location.Scope == tx.scope {
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
		if key, ok := s.index.unique[claim]; ok {
			if _, changed := tx.changes[key]; changed {
				return entityKey{}, fault.ErrNotFound
			}
			if tx.deletedScope && s.index.latest[key].location.Scope == tx.scope {
				return entityKey{}, fault.ErrNotFound
			}
		}
	}
	key, ok := s.index.unique[claim]
	if !ok {
		return entityKey{}, fault.ErrNotFound
	}
	return key, nil
}

func (s *Store) entry(ctx context.Context, key entityKey) (indexEntry, bool) {
	if tx := s.tx(ctx); tx != nil {
		if changed, ok := tx.changes[key]; ok && !tx.deletedScope {
			return changed.entry, true
		}
		if tx.deletedScope && s.index.latest[key].location.Scope == tx.scope {
			return indexEntry{}, false
		}
	}
	entry, ok := s.index.latest[key]
	return entry, ok
}

func (s *Store) publish(tx *transaction, offset, length int64) {
	if tx.deletedScope {
		for key := range s.index.groups[scopeKey(tx.scope)] {
			if key.collection == "agent" {
				delete(s.index.maxQueue, key.id)
			}
			s.index.remove(key)
		}
		for key := range s.index.missing {
			if key.scope == tx.scope {
				delete(s.index.missing, key)
			}
		}
		delete(s.index.hasMessages, tx.scope)
		s.messages.remove(tx.scope)
		s.index.deleted[tx.scope] = true
	} else {
		// 先移除所有旧查询键，再添加最终键，允许一个事务内合法的唯一键交换。
		for key := range tx.changes {
			if key.collection != "message" {
				s.index.remove(key)
			}
		}
		for key, change := range tx.changes {
			if key.collection == "message" {
				continue
			}
			entry := change.entry
			entry.location.Offset, entry.location.Length = offset, length
			s.index.add(key, entry)
			if key.collection == "session" {
				for _, group := range entry.groups {
					if group.kind == "sessions-project" {
						s.sessionProjects[tx.scope] = group.owner
						break
					}
				}
			}
		}
		for n, event := range tx.events {
			if event.Collection != "message" {
				continue
			}
			var record messageRecord
			_ = decodeJSON(event.Payload, &record) // 提交前已校验并构建成功。
			ref := recordRef{
				Scope:      tx.scope,
				Offset:     offset,
				Length:     length,
				EventIndex: n,
				Sequence:   event.Sequence,
			}
			s.messages.append(tx.scope, record, ref)
			if tx.replayMessages != nil {
				tx.replayMessages.add(record, ref)
			}
			s.index.hasMessages[tx.scope] = true
		}
		for key, value := range tx.receiptChanges {
			if value == nil {
				delete(s.index.missing, key)
			} else {
				s.index.missing[key] = *value
			}
		}
	}
	s.sequences[tx.scope] = tx.nextSequence
	s.lengths[tx.scope] = offset + length
}
