package coordinator

import (
	"context"
	"maps"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
	"strings"
	"time"
)

type BindingView struct {
	BindingID string
	Session   sessionmodel.SessionInfo
	Branch    sessionmodel.Branch
	Tasks     []sessionmodel.Task
}

type BranchView struct {
	Branch  sessionmodel.Branch `json:"branch"`
	Summary string              `json:"summary"`
}

// CreateSession 将创建与 Hook 收尾纳入关闭等待，并排除同会话的加载和删除。
func (s *Coordinator) CreateSession(ctx context.Context, input sessionservice.CreateInput) (sessionservice.CreateResult, error) {
	if err := s.enter(); err != nil {
		return sessionservice.CreateResult{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, input.SessionID)
	if err != nil {
		return sessionservice.CreateResult{}, err
	}
	defer unlock()
	return s.sessions.CreateSession(ctx, input)
}

// SelectSession 加载会话并返回当前写入绑定；前端按 SessionID 持有唯一会话页。
func (s *Coordinator) SelectSession(ctx context.Context, id identity.SessionID, branchID identity.BranchID) (BindingView, error) {
	if err := s.enter(); err != nil {
		return BindingView{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return BindingView{}, err
	}
	defer unlock()
	s.mu.Lock()
	binding := s.bindings[id]
	if binding != nil && (binding.closing || branchID != "" && binding.branchID != branchID) {
		s.mu.Unlock()
		return BindingView{}, errorcode.ErrRequestConflict
	}
	bindingID := ""
	if binding != nil {
		branchID, bindingID = binding.branchID, binding.id
	}
	s.mu.Unlock()
	if _, err := s.sessions.LoadSession(ctx, id); err != nil {
		return BindingView{}, err
	}
	if branchID == "" {
		branch, err := s.sessions.DefaultBranch(ctx, id)
		if err != nil {
			return BindingView{}, err
		}
		branchID = branch.ID
	}
	return s.sessionView(ctx, id, branchID, bindingID)
}

func (s *Coordinator) CreateBranch(ctx context.Context, id identity.SessionID, branchID identity.BranchID, bindingID string, input sessionmodel.BranchInput) (BindingView, error) {
	if err := s.enter(); err != nil {
		return BindingView{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return BindingView{}, err
	}
	defer unlock()
	requestKey := id.String() + ":" + branchID.String() + ":" + input.RequestID
	s.mu.Lock()
	result := s.branchResults[requestKey]
	s.mu.Unlock()
	if result != "" {
		if _, err := s.sessions.ConfirmBranchRequest(ctx, id, input); err != nil {
			return BindingView{}, err
		}
		return s.sessionView(ctx, id, identity.BranchID(result), "")
	}
	binding, err := s.navigationBinding(id, branchID, bindingID)
	if err != nil {
		return BindingView{}, err
	}
	source, err := s.sessionView(ctx, id, branchID, bindingID)
	if err != nil {
		return BindingView{}, err
	}
	branch, err := s.sessions.CreateBranch(ctx, id, source.Branch, input)
	if err != nil {
		return BindingView{}, err
	}
	s.mu.Lock()
	if binding != nil {
		s.releaseBindingLocked(binding)
	}
	s.branchResults[requestKey] = branch.ID.String()
	s.mu.Unlock()
	return s.sessionView(ctx, id, branch.ID, "")
}

func (s *Coordinator) SwitchBranch(ctx context.Context, id identity.SessionID, branchID identity.BranchID, bindingID, name string) (BindingView, error) {
	if err := s.enter(); err != nil {
		return BindingView{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return BindingView{}, err
	}
	defer unlock()
	binding, err := s.navigationBinding(id, branchID, bindingID)
	if err != nil {
		return BindingView{}, err
	}
	branches, err := s.sessions.Branches(ctx, id)
	if err != nil {
		return BindingView{}, err
	}
	for _, branch := range branches {
		if branch.Name != name {
			continue
		}
		if branch.ID == branchID {
			return s.sessionView(ctx, id, branchID, bindingID)
		}
		view, err := s.sessionView(ctx, id, branch.ID, "")
		if err != nil {
			return BindingView{}, err
		}
		if binding != nil {
			s.mu.Lock()
			s.releaseBindingLocked(binding)
			s.mu.Unlock()
		}
		return view, nil
	}
	return BindingView{}, errorcode.ErrNotFound
}

func (s *Coordinator) ListBranches(ctx context.Context, id identity.SessionID) ([]BranchView, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	branches, err := s.sessions.Branches(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]BranchView, 0, len(branches))
	for _, branch := range branches {
		summary := ""
		if branch.HeadEntryID != "" {
			path, err := s.sessions.Path(ctx, id, branch.HeadEntryID)
			if err != nil {
				return nil, err
			}
			if len(path) > 0 {
				summary = path[len(path)-1].TextContent()
				runes := []rune(summary)
				if len(runes) > 100 {
					summary = string(runes[:100]) + "…"
				}
			}
		}
		result = append(result, BranchView{
			Branch:  branch,
			Summary: summary,
		})
	}
	return result, nil
}

func (s *Coordinator) Tree(ctx context.Context, id identity.SessionID) ([]sessionmodel.TreeNode, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.sessions.Tree(ctx, id)
}

func (s *Coordinator) EntriesAt(ctx context.Context, id identity.SessionID, head string) ([]sessionentry.ContextEntry, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.sessions.Path(ctx, id, head)
}

func (s *Coordinator) sessionView(ctx context.Context, id identity.SessionID, branchID identity.BranchID, bindingID string) (BindingView, error) {
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return BindingView{}, err
	}
	branches, err := s.sessions.Branches(ctx, id)
	if err != nil {
		return BindingView{}, err
	}
	for _, branch := range branches {
		if branch.ID != branchID {
			continue
		}
		view := BindingView{
			BindingID: bindingID,
			Session:   info,
			Branch:    branch,
		}
		s.mu.Lock()
		if binding := s.bindings[id]; binding != nil && binding.id == bindingID {
			view.Tasks = binding.queue.List(0)
		}
		s.mu.Unlock()
		return view, nil
	}
	return BindingView{}, errorcode.ErrNotFound
}

// bindSubmission 为首次输入建立会话唯一的写入绑定；调用方持有会话命令锁。
func (s *Coordinator) bindSubmission(ctx context.Context, input sessionservice.TaskInput) (*sessionBinding, error) {
	owner, branch, err := s.sessions.Prepare(ctx, input.SessionID, input.BranchID)
	if err != nil {
		return nil, err
	}
	binding := &sessionBinding{
		id:        input.BindingID,
		sessionID: input.SessionID,
		branchID:  branch.ID,
		session:   owner,
	}
	s.mu.Lock()
	s.bindings[input.SessionID] = binding
	s.mu.Unlock()
	return binding, nil
}

func (s *Coordinator) releaseBindingLocked(binding *sessionBinding) {
	delete(s.bindings, binding.sessionID)
	binding.queue.Clear()
}

func (s *Coordinator) releaseSessionStateLocked(id identity.SessionID) {
	if binding := s.bindings[id]; binding != nil {
		s.releaseBindingLocked(binding)
	}
	maps.DeleteFunc(s.branchResults, func(key, _ string) bool { return strings.HasPrefix(key, id.String()+":") })
}

func (s *Coordinator) navigationBinding(id identity.SessionID, branchID identity.BranchID, bindingID string) (*sessionBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding := s.bindings[id]
	if binding == nil && bindingID == "" {
		return nil, nil
	}
	if binding == nil || binding.id != bindingID || binding.branchID != branchID || binding.closing {
		return nil, errorcode.ErrRequestConflict
	}
	if s.runs[runKey{identity.AgentID(binding.sessionID), binding.branchID}] != nil || len(binding.queue.List(0)) > 0 {
		return nil, agentmodel.ErrExecuting
	}
	return binding, nil
}

// CloseSession 停止指定会话的全部执行、清空待办，等待收尾后释放会话内存。
func (s *Coordinator) CloseSession(ctx context.Context, id identity.SessionID) (err error) {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	binding, notification, err := s.beginSessionClose(ctx, id)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil && binding != nil {
			s.mu.Lock()
			if s.bindings[id] == binding {
				binding.closing = false
			}
			s.mu.Unlock()
		}
	}()
	// 关闭失败时前端仍保留会话，队列清空也必须同步到界面。
	if notification != nil {
		s.loop.Emitter.Emit(*notification)
	}
	tick := time.Tick(20 * time.Millisecond)
	for {
		done, err := s.finishSessionClose(ctx, id)
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick:
		}
	}
}

func (s *Coordinator) beginSessionClose(ctx context.Context, id identity.SessionID) (*sessionBinding, *coreevent.Event, error) {
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, active := range s.runs {
		if active.sessionID == id {
			active.cancel(context.Canceled)
		}
	}
	binding := s.bindings[id]
	if binding == nil {
		return nil, nil, nil
	}
	binding.closing = true
	binding.queue.Clear()
	return binding, new(taskQueueEvent(binding)), nil
}

func (s *Coordinator) finishSessionClose(ctx context.Context, id identity.SessionID) (bool, error) {
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return false, err
	}
	defer unlock()
	s.mu.Lock()
	for _, active := range s.runs {
		if active.sessionID == id {
			s.mu.Unlock()
			return false, nil
		}
	}
	s.mu.Unlock()
	if err := s.sessions.ReleaseSession(ctx, id); err != nil {
		return false, err
	}
	s.mu.Lock()
	s.releaseSessionStateLocked(id)
	s.mu.Unlock()
	return true, nil
}
