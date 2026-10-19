package jsonl

import (
	"bufio"
	"container/list"
	"context"
	"errors"
	"io"
	"os"
)

type messageRef struct {
	id       string
	sequence uint64
	location recordRef
}

type sessionMessages struct {
	byID    map[string]messageRef
	streams map[string][]messageRef
}

func newSessionMessages() *sessionMessages {
	return &sessionMessages{byID: make(map[string]messageRef), streams: make(map[string][]messageRef)}
}
func (i *sessionMessages) add(record messageRecord, ref recordRef) {
	message := messageRef{id: record.Data.ID, sequence: record.Data.Sequence, location: ref}
	i.byID[messageKey(record.OwnerID, message.id)] = message
	i.streams[record.OwnerID] = append(i.streams[record.OwnerID], message)
}

type cachedSession struct {
	scope string
	index *sessionMessages
}

type messageCache struct {
	limit    int
	entries  int
	sessions map[string]*list.Element
	lru      *list.List
}

func newMessageCache(limit int) messageCache {
	return messageCache{limit: limit, sessions: make(map[string]*list.Element), lru: list.New()}
}
func (c *messageCache) remove(scope string) {
	if entry := c.sessions[scope]; entry != nil {
		c.entries -= len(entry.Value.(cachedSession).index.byID)
		c.lru.Remove(entry)
		delete(c.sessions, scope)
	}
}
func (c *messageCache) trim() {
	for c.entries > c.limit && c.lru.Len() > 0 {
		c.remove(c.lru.Back().Value.(cachedSession).scope)
	}
}
func (c *messageCache) append(scope string, record messageRecord, ref recordRef) {
	if entry := c.sessions[scope]; entry != nil {
		entry.Value.(cachedSession).index.add(record, ref)
		c.entries++
		c.trim()
	}
}

func (s *Store) messageIndex(ctx context.Context, scope string) (*sessionMessages, error) {
	if tx := s.tx(ctx); tx != nil && tx.replayMessages != nil {
		return tx.replayMessages, nil
	}
	if entry := s.messages.sessions[scope]; entry != nil {
		s.messages.lru.MoveToFront(entry)
		return entry.Value.(cachedSession).index, nil
	}
	index, err := s.scanMessages(ctx, scope)
	if err != nil {
		return nil, err
	}
	if len(index.byID) <= s.messages.limit {
		s.messages.sessions[scope] = s.messages.lru.PushFront(cachedSession{scope: scope, index: index})
		s.messages.entries += len(index.byID)
		s.messages.trim()
	}
	return index, nil
}

func (s *Store) scanMessages(ctx context.Context, scope string) (*sessionMessages, error) {
	index := newSessionMessages()
	if s.lengths[scope] == 0 {
		return index, nil
	}
	path, err := s.path(ctx, scope)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("log must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(io.NewSectionReader(file, 0, s.lengths[scope]))
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(line) == 0 {
			break
		}
		if err != nil {
			return nil, err
		}
		var record batch
		if err := decodeJSON(line, &record); err != nil {
			return nil, err
		}
		for n, event := range record.Events {
			if event.Type == "session.deleted" {
				index = newSessionMessages()
			}
			if event.Collection != "message" {
				continue
			}
			var value messageRecord
			if err := decodeJSON(event.Payload, &value); err != nil {
				return nil, err
			}
			if event.Key != messageKey(value.OwnerID, value.Data.ID) || value.Data.Sequence != event.Sequence {
				return nil, errors.New("corrupt message identity or sequence")
			}
			index.add(value, recordRef{
				Scope:      scope,
				Offset:     offset,
				Length:     int64(len(line)),
				EventIndex: n,
				Sequence:   event.Sequence,
			})
		}
		offset += int64(len(line))
	}
	return index, nil
}
