package session

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"time"
)

// AppendEntry 接纳完整 Entry；模型请求的加工结果不经过此入口。
func (s *Session) AppendEntry(ctx context.Context, branchID identity.BranchID, entry sessionentry.ContextEntry) (stored *sessionentry.ContextEntry, inserted bool, err error) {
	err = s.use(ctx, func() error {
		if prior, found := s.model.Entry(entry.ID); found {
			entry.BranchID, entry.ParentID = branchID, prior.ParentID
			var appendErr error
			stored, inserted, appendErr = s.appendEntry(ctx, entry)
			return appendErr
		}
		branch := s.model.Branch(branchID)
		if branch == nil {
			return errorcode.ErrNotFound
		}
		entry.BranchID, entry.ParentID = branch.ID, branch.HeadEntryID
		if err := validateEntry(s.model, entry); err != nil {
			return err
		}
		var appendErr error
		stored, inserted, appendErr = s.appendEntry(ctx, entry)
		return appendErr
	})
	return
}

func (s *Session) appendEntry(ctx context.Context, entry sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	if prior, found := s.model.Entry(entry.ID); found {
		entry.Sequence, entry.CreatedAt = prior.Sequence, prior.CreatedAt
		if !reflect.DeepEqual(*prior, entry) {
			return nil, false, errorcode.ErrRequestConflict
		}
		return prior, false, nil
	}
	entry.Sequence = uint64(len(s.model.Entries)) + 1
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if err := s.appendEvent(ctx, "entry.appended", "entry", entry.ID, entry); err != nil {
		return nil, false, err
	}
	return s.model.AcceptEntry(entry), true, nil
}

// BranchEntries 返回已提交的完整分支路径；Run 据此构建 Context 和执行证据。
func (s *Session) BranchEntries(ctx context.Context, branchID identity.BranchID) (entries []*sessionentry.ContextEntry, err error) {
	err = s.use(ctx, func() error {
		branch := s.model.Branch(branchID)
		if branch == nil {
			return errorcode.ErrNotFound
		}
		entries = s.model.Path(branch.HeadEntryID)
		return nil
	})
	return
}

func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID, branchID identity.BranchID) (page sessionentry.List, err error) {
	owner, err := s.openedSession(ctx, sessionID)
	if err != nil {
		return page, err
	}
	err = owner.use(ctx, func() error {
		branch := owner.model.Branch(branchID)
		if branch == nil {
			return errorcode.ErrNotFound
		}
		page.Entries = sessionentry.Values(owner.model.Path(branch.HeadEntryID))
		return nil
	})
	return
}

func validateContextEntry(entry sessionentry.ContextEntry) error {
	if entry.BranchID == "" {
		return errors.New("entry branch is required")
	}
	return entry.Validate()
}
