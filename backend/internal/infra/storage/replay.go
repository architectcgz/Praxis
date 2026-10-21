package storage

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/fault"
	"praxis/internal/infra/jsonl"
)

func (s *Store) replay(ctx context.Context, scope jsonl.Scope) error {
	// 终止事件顺序仅在本次文件恢复期间校验，不保留运行期删除状态。
	deleted := false
	return s.log.Replay(ctx, scope, func(commit jsonl.Commit) error {
		if deleted {
			return errors.New("events follow a deleted session")
		}
		tx := s.newTransaction()
		defer func() { tx.active = false }()
		tx.scope, tx.nextSequence = scope.ID, s.log.Sequence(scope.ID)
		txCtx := context.WithValue(ctx, transactionKey{}, tx)
		for _, event := range commit.Events {
			var entry stateEntry
			if event.Type != "session.deleted" {
				var err error
				entry, err = restoreStateEntry(scope.ID, event)
				if err != nil {
					return fmt.Errorf("seq=%d: %w", event.Sequence, err)
				}
				prior, err := s.payload(txCtx, eventKey(scope.ID, event))
				if err == nil {
					if err := immutable(event.Collection, prior, event.Payload); err != nil {
						return err
					}
				} else if !errors.Is(err, fault.ErrNotFound) {
					return err
				}
			}
			if err := s.stage(txCtx, event, entry); err != nil {
				return fmt.Errorf("seq=%d: %w", event.Sequence, err)
			}
		}
		if len(tx.events) != len(commit.Events) {
			return errors.New("redundant or duplicate event")
		}
		// 会话第一次提交时日志游标尚未发布，目录归属直接与当前文件对比。
		for key, change := range tx.changes {
			if key.collection == "session" {
				for _, group := range change.entry.groups {
					if group.kind == "sessions-project" && group.owner != scope.ProjectID {
						return errors.New("session project does not match log directory")
					}
				}
			}
		}
		if err := s.validateRestoredTransaction(txCtx); err != nil {
			return err
		}
		s.publish(tx)
		deleted = tx.deletedScope
		return nil
	})
}
