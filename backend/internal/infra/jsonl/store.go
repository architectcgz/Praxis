// Package jsonl 实现单进程本地日志及其可重建内存投影。
package jsonl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/utils/pathutil"
)

// Event 是一个领域对象变更；Session 归属只由所在文件名决定。
type Event struct {
	Sequence   uint64          `json:"seq"`
	ID         string          `json:"event_id"`
	Type       string          `json:"type"`
	Collection string          `json:"collection"`
	Key        string          `json:"key,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// 一个事务只写入一行，换行是提交边界；不完整尾行不包含已提交事实。
type batch struct {
	Events []Event `json:"events"`
}

type objectKey struct{ collection, id string }
type object struct {
	scope string
	data  json.RawMessage
}
type transaction struct {
	store     *Store
	objects   map[objectKey]object
	sequences map[string]uint64
	scope     string
	events    []Event
}
type transactionKey struct{}

// Store 串行提交本地变更，并保证读取到同一个已提交投影。
type Store struct {
	mu        sync.Mutex
	root      string
	objects   map[objectKey]object
	sequences map[string]uint64
	lock      *os.File
	closed    bool
	fault     error
}

// Open 校验目录并恢复日志；损坏的完整记录会阻止启动，不完整尾行先留存再截断。
func Open(ctx context.Context, root string) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("日志 context 不能为空")
	}
	if !pathutil.IsAbsoluteNormalized(root) {
		return nil, errors.New("日志目录必须是规范化绝对路径")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(root, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("数据目录已被占用: %w", err)
	}
	s := &Store{
		root:      root,
		objects:   make(map[objectKey]object),
		sequences: make(map[string]uint64),
		lock:      lock,
	}
	paths := []string{filepath.Join(root, "projects.jsonl")}
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".jsonl") {
				paths = append(paths, filepath.Join(root, "sessions", entry.Name()))
			}
		}
	}
	if err == nil {
		for _, path := range paths {
			if err = ctx.Err(); err != nil {
				break
			}
			if err = s.replay(path); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = s.validate(s.objects)
	}
	if err != nil {
		s.Close(context.Background())
		return nil, err
	}
	return s, nil
}

func validScope(scope string) bool {
	// 小写 ASCII 身份避免 Windows 大小写折叠导致两个 Session 共用一个文件。
	if scope == "" || len(scope) > 180 || !filepath.IsLocal(scope) {
		return false
	}
	for _, char := range scope {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}
func (s *Store) path(scope string) (string, error) {
	if scope == "" {
		return filepath.Join(s.root, "projects.jsonl"), nil
	}
	if !validScope(scope) {
		return "", errors.New("Session ID 不是合法文件名")
	}
	return filepath.Join(s.root, "sessions", scope+".jsonl"), nil
}
func (s *Store) tx(ctx context.Context) *transaction {
	if ctx == nil {
		return nil
	}
	tx, _ := ctx.Value(transactionKey{}).(*transaction)
	if tx != nil && tx.store == s {
		return tx
	}
	return nil
}

// InTx 将同一 Session 的关联更新作为一行提交；跨 Session 写入被拒绝。
func (s *Store) InTx(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil || fn == nil {
		return errors.New("事务 context 和回调不能为空")
	}
	if s.tx(ctx) != nil {
		return fn(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("日志已关闭")
	}
	if s.fault != nil {
		return s.fault
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// ponytail: 本地事务复制索引并校验全量引用；数据量增大后改为变更集校验。
	tx := &transaction{store: s, objects: maps.Clone(s.objects), sequences: maps.Clone(s.sequences)}
	if err := fn(context.WithValue(ctx, transactionKey{}, tx)); err != nil {
		return err
	}
	if len(tx.events) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validate(tx.objects); err != nil {
		return err
	}
	encoded, err := json.Marshal(batch{Events: tx.events})
	if err != nil {
		return err
	}
	path, err := s.path(tx.scope)
	if err != nil {
		return err
	}
	if err = s.append(path, append(encoded, '\n')); err != nil {
		return err
	}
	s.objects, s.sequences = tx.objects, tx.sequences
	return nil
}

func (s *Store) append(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		// 回滚失败后封锁写入，必须重新打开日志确定提交结果。
		rollback := errors.Join(file.Truncate(offset), file.Sync())
		if rollback != nil {
			s.fault = fmt.Errorf("日志写入结果未知，需重启恢复: %w", errors.Join(err, rollback))
		}
		return errors.Join(err, rollback)
	}
	return nil
}

func (s *Store) change(ctx context.Context, scope, collection, id, kind string, data json.RawMessage) error {
	tx := s.tx(ctx)
	if tx == nil {
		return errors.New("领域变更必须位于事务内")
	}
	if _, err := s.path(scope); err != nil {
		return err
	}
	if len(tx.events) > 0 && tx.scope != scope {
		return errors.New("一次事务不能写入多个日志文件")
	}
	key := projectionKey(scope, collection, id)
	if previous, ok := tx.objects[key]; ok {
		if previous.scope != scope {
			return contracts.ErrRequestConflict
		}
		if bytes.Equal(previous.data, data) {
			return nil
		}
	}
	seq := tx.sequences[scope] + 1
	event := Event{
		Sequence:   seq,
		ID:         fmt.Sprintf("event:%d", seq),
		Type:       kind,
		Collection: collection,
		Key:        id,
		CreatedAt:  time.Now().UTC(),
		Payload:    data,
	}
	if collection == "session" {
		event.Key = ""
	}
	if err := validateEvent(scope, event); err != nil {
		return err
	}
	if err := apply(tx.objects, scope, event); err != nil {
		return err
	}
	tx.scope = scope
	tx.sequences[scope] = seq
	tx.events = append(tx.events, event)
	return nil
}

func apply(objects map[objectKey]object, scope string, event Event) error {
	if event.Type == "session.deleted" {
		for key, value := range objects {
			if value.scope == scope {
				delete(objects, key)
			}
		}
		return nil
	}
	key := projectionKey(scope, event.Collection, event.Key)
	if event.Collection == "session" {
		key.id = scope
	}
	if value, ok := objects[key]; ok {
		if value.scope != scope {
			return contracts.ErrRequestConflict
		}
		if err := immutable(event.Collection, value.data, event.Payload); err != nil {
			return err
		}
	}
	objects[key] = object{scope: scope, data: bytes.Clone(event.Payload)}
	return nil
}

func (s *Store) replay(path string) error {
	scope := ""
	if filepath.Dir(path) == filepath.Join(s.root, "sessions") {
		scope = strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if !validScope(scope) {
			return fmt.Errorf("非法日志文件名: %s", path)
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("日志必须是普通文件: %s", path)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var offset int64
	for {
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 {
				backup, err := os.CreateTemp(s.root, "incomplete-tail-*")
				if err != nil {
					return err
				}
				_, writeErr := backup.Write(line)
				syncErr := backup.Sync()
				closeErr := backup.Close()
				if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
					return err
				}
				if err = file.Truncate(offset); err != nil {
					return err
				}
				if err = file.Sync(); err != nil {
					return err
				}
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
		var record batch
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&record); err != nil {
			return fmt.Errorf("%s 偏移 %d: %w", path, offset, err)
		}
		if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return fmt.Errorf("%s 偏移 %d: 多余 JSON 数据", path, offset)
		}
		if len(record.Events) == 0 {
			return fmt.Errorf("%s 偏移 %d: 空提交", path, offset)
		}
		for _, event := range record.Events {
			if event.Sequence != s.sequences[scope]+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
				return fmt.Errorf("%s 偏移 %d: 事件身份或顺序非法", path, offset)
			}
			if err = validateEvent(scope, event); err != nil {
				return fmt.Errorf("%s seq=%d: %w", path, event.Sequence, err)
			}
			if err = validateRestoredEvent(scope, event); err != nil {
				return fmt.Errorf("%s seq=%d: %w", path, event.Sequence, err)
			}
			if err = apply(s.objects, scope, event); err != nil {
				return err
			}
			s.sequences[scope] = event.Sequence
		}
		if err := s.validate(s.objects); err != nil {
			return fmt.Errorf("%s 偏移 %d: %w", path, offset, err)
		}
		offset += int64(len(line))
	}
}

// Close 关闭写入入口并释放操作系统锁；进程异常退出时锁由系统释放。
func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("关闭 context 不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.lock.Close()
}
