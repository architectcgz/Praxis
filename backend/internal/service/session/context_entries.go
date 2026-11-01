package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"time"
)

// AppendEntry 接纳完整 Entry；模型请求的加工结果不经过此入口。
func (s *Session) AppendEntry(ctx context.Context, branchID identity.BranchID, entry sessionentry.ContextEntry) (stored *sessionentry.ContextEntry, inserted bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkState(ctx); err != nil {
		return nil, false, err
	}
	if prior, found := s.data.Entry(entry.ID); found {
		entry.BranchID, entry.ParentID = branchID, prior.ParentID
		return s.appendEntry(ctx, entry)
	}
	branch := s.data.Branch(branchID)
	if branch == nil {
		return nil, false, errorcode.ErrNotFound
	}
	entry.BranchID, entry.ParentID = branch.ID, branch.HeadEntryID
	if err := validateEntry(s.data, entry); err != nil {
		return nil, false, err
	}
	return s.appendEntry(ctx, entry)
}

func (s *Session) appendEntry(ctx context.Context, entry sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	if prior, found := s.data.Entry(entry.ID); found {
		entry.Sequence, entry.CreatedAt = prior.Sequence, prior.CreatedAt
		if !reflect.DeepEqual(*prior, entry) {
			return nil, false, errorcode.ErrRequestConflict
		}
		return prior, false, nil
	}
	entry.Sequence = uint64(len(s.data.Entries)) + 1
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if err := s.appendEvent(ctx, "entry.appended", "entry", entry.ID, entry); err != nil {
		return nil, false, err
	}
	return s.data.AcceptEntry(entry), true, nil
}

// BranchEntries 返回已提交的完整分支路径；Run 据此构建 Context 和执行证据。
func (s *Session) BranchEntries(ctx context.Context, branchID identity.BranchID) (entries []*sessionentry.ContextEntry, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.checkState(ctx); err != nil {
		return nil, err
	}
	branch := s.data.Branch(branchID)
	if branch == nil {
		return nil, errorcode.ErrNotFound
	}
	return s.data.Path(branch.HeadEntryID), nil
}

func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID, branchID identity.BranchID) (page sessionentry.List, err error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return page, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return page, err
	}
	branch := owner.data.Branch(branchID)
	if branch == nil {
		return page, errorcode.ErrNotFound
	}
	page.Entries = sessionentry.Values(owner.data.Path(branch.HeadEntryID))
	return page, nil
}
