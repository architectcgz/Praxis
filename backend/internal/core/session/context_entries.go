package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
)

// AppendEntry 先将完整 Entry 接纳到内存，再同步追加日志；写入失败不回滚内存。
// inserted 表示内存是否新增条目，与日志写入是否成功无关；重复条目不重复写入。
// 模型请求的加工结果不经过此入口。
func (s *Session) AppendEntry(ctx context.Context, branchID identity.BranchID, entry sessionentry.ContextEntry) (stored *sessionentry.ContextEntry, inserted bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	branch := s.data.Branch(branchID)
	if branch == nil {
		return nil, false, errorcode.ErrNotFound
	}
	if prior, found := s.data.Entry(entry.ID); found {
		entry.ParentID, entry.CreatedAt = prior.ParentID, prior.CreatedAt
		if !reflect.DeepEqual(*prior, entry) || !s.data.hasAncestor(branch.HeadEntryID, prior.ID) {
			return nil, false, errorcode.ErrRequestConflict
		}
		if index := s.data.RunIndex(identity.RunID(entry.RunID)); index >= 0 && s.data.Runs[index].BranchID != branchID {
			return nil, false, errorcode.ErrRequestConflict
		}
		return prior, false, nil
	}
	entry.ParentID = branch.HeadEntryID
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.clock.Now()
	}
	stored = s.data.AcceptEntry(branchID, entry)
	err = s.log.AppendEvent(ctx, s.data.ID, Event{
		Type:      EventEntryAppended,
		Key:       entry.ID,
		BranchID:  branchID,
		CreatedAt: s.clock.Now(),
		Payload:   entry,
	})
	return stored, true, err
}

// BranchEntries 返回内存中的完整分支路径；Run 据此构建 Context 和执行证据。
func (s *Session) BranchEntries(ctx context.Context, branchID identity.BranchID) (entries []*sessionentry.ContextEntry, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	branch := s.data.Branch(branchID)
	if branch == nil {
		return nil, errorcode.ErrNotFound
	}
	return s.data.Path(branch.HeadEntryID), nil
}
