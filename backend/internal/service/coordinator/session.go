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
	Branch   sessionmodel.Branch `json:"branch"`
	Occupied bool                `json:"occupied"`
	Summary  string              `json:"summary"`
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

// SelectSession 加载会话并登记页面使用期；写入绑定仍由首次提交输入创建。
func (s *Coordinator) SelectSession(ctx context.Context, id identity.SessionID, branchID identity.BranchID, bindingID, pageID string) (BindingView, error) {
	if err := s.enter(); err != nil {
		return BindingView{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return BindingView{}, err
	}
	defer unlock()
	if bindingID != "" {
		s.mu.Lock()
		binding := s.bindings[bindingID]
		valid := binding != nil && binding.sessionID == id && !binding.closing && (branchID == "" || binding.branchID == branchID)
		if valid {
			branchID = binding.branchID
		}
		s.mu.Unlock()
		if !valid {
			return BindingView{}, errorcode.ErrRequestConflict
		}
	}
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
	view, err := s.sessionView(ctx, id, branchID, bindingID)
	if err != nil {
		return BindingView{}, err
	}
	s.mu.Lock()
	s.pages[pageID] = id
	s.mu.Unlock()
	return view, nil
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

func (s *Coordinator) ListBranches(ctx context.Context, id identity.SessionID, bindingID string) ([]BranchView, error) {
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
		s.mu.Lock()
		owner := s.branchOwners[branchKey{id, branch.ID}]
		s.mu.Unlock()
		result = append(result, BranchView{
			Branch:   branch,
			Occupied: owner != "" && owner != bindingID,
			Summary:  summary,
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
		if binding := s.bindings[bindingID]; binding != nil {
			view.Tasks = binding.queue.List(0)
		}
		s.mu.Unlock()
		return view, nil
	}
	return BindingView{}, errorcode.ErrNotFound
}

// bindSubmission 在真正写入前取得分支所有权；另一个页面已有写入绑定时才分叉。
// 调用方持有会话命令锁，使占用判断、分叉和写入权登记连续完成。
func (s *Coordinator) bindSubmission(ctx context.Context, input sessionservice.TaskInput) (*sessionBinding, error) {
	view, err := s.sessionView(ctx, input.SessionID, input.BranchID, "")
	if err != nil {
		return nil, err
	}
	branchID := input.BranchID
	s.mu.Lock()
	occupied := s.branchOwners[branchKey{input.SessionID, branchID}] != ""
	s.mu.Unlock()
	if occupied {
		head, err := s.sessions.SafeHead(ctx, input.SessionID, branchID)
		if err != nil {
			return nil, err
		}
		branch, err := s.sessions.CreateBranch(ctx, input.SessionID, view.Branch, sessionmodel.BranchInput{RequestID: input.BindingID, EntryID: head})
		if err != nil {
			return nil, err
		}
		branchID = branch.ID
	}
	owner, branch, err := s.sessions.Prepare(ctx, input.SessionID, branchID)
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
	defer s.mu.Unlock()
	if s.bindings[binding.id] != nil {
		return nil, errorcode.ErrRequestConflict
	}
	s.bindings[binding.id] = binding
	s.branchOwners[branchKey{binding.sessionID, binding.branchID}] = binding.id
	return binding, nil
}

func (s *Coordinator) releaseBindingLocked(binding *sessionBinding) {
	delete(s.branchOwners, branchKey{binding.sessionID, binding.branchID})
	delete(s.bindings, binding.id)
	binding.queue.Clear()
}

func (s *Coordinator) releaseSessionStateLocked(id identity.SessionID) {
	for _, binding := range s.bindings {
		if binding.sessionID == id {
			s.releaseBindingLocked(binding)
		}
	}
	maps.DeleteFunc(s.pages, func(_ string, sessionID identity.SessionID) bool { return sessionID == id })
	maps.DeleteFunc(s.branchResults, func(key, _ string) bool { return strings.HasPrefix(key, id.String()+":") })
}

func (s *Coordinator) navigationBinding(id identity.SessionID, branchID identity.BranchID, bindingID string) (*sessionBinding, error) {
	if bindingID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding := s.bindings[bindingID]
	if binding == nil || binding.sessionID != id || binding.branchID != branchID || binding.closing {
		return nil, errorcode.ErrRequestConflict
	}
	if s.runs[runKey{identity.AgentID(binding.sessionID), binding.branchID}] != nil || len(binding.queue.List(0)) > 0 {
		return nil, agentmodel.ErrExecuting
	}
	return binding, nil
}

// CloseSession 等待本页执行收尾；最后一个页面必须等整个会话的执行退出后才释放数据。
func (s *Coordinator) CloseSession(ctx context.Context, pageID, bindingID string) (err error) {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	s.mu.Lock()
	id, opened := s.pages[pageID]
	s.mu.Unlock()
	if !opened {
		return nil
	}
	binding, notification, err := s.beginSessionClose(ctx, id, pageID, bindingID)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil && binding != nil {
			s.mu.Lock()
			if s.bindings[binding.id] == binding {
				binding.closing = false
			}
			s.mu.Unlock()
		}
	}()
	// 关闭失败时页面仍然保留，队列清空也必须同步到界面。
	if notification != nil {
		s.loop.Emitter.Emit(*notification)
	}
	tick := time.Tick(20 * time.Millisecond)
	for {
		done, err := s.finishSessionClose(ctx, id, pageID, binding)
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

func (s *Coordinator) beginSessionClose(ctx context.Context, id identity.SessionID, pageID, bindingID string) (*sessionBinding, *coreevent.Event, error) {
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pages[pageID] != id {
		return nil, nil, nil
	}
	binding := s.bindings[bindingID]
	if binding == nil {
		return nil, nil, nil
	}
	if binding.sessionID != id {
		return nil, nil, errorcode.ErrRequestConflict
	}
	binding.closing = true
	for _, active := range s.runs {
		if active.bindingID == bindingID {
			active.cancel(context.Canceled)
		}
	}
	binding.queue.Clear()
	return binding, new(taskQueueEvent(binding)), nil
}

func (s *Coordinator) finishSessionClose(ctx context.Context, id identity.SessionID, pageID string, binding *sessionBinding) (bool, error) {
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return false, err
	}
	defer unlock()
	s.mu.Lock()
	if s.pages[pageID] != id {
		s.mu.Unlock()
		return true, nil
	}
	shared := false
	for otherPage, sessionID := range s.pages {
		if otherPage != pageID && sessionID == id {
			shared = true
			break
		}
	}
	busy := false
	for _, active := range s.runs {
		if !shared && active.sessionID == id {
			active.cancel(context.Canceled)
			busy = true
		} else if binding != nil && active.bindingID == binding.id {
			busy = true
		}
	}
	s.mu.Unlock()
	if busy {
		return false, nil
	}
	if !shared {
		if err := s.sessions.ReleaseSession(ctx, id); err != nil {
			return false, err
		}
	}
	s.mu.Lock()
	if !shared {
		s.releaseSessionStateLocked(id)
	} else {
		if binding != nil {
			s.releaseBindingLocked(binding)
		}
		delete(s.pages, pageID)
	}
	s.mu.Unlock()
	return true, nil
}
