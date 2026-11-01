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

// CreateSession 将创建与 Hook 收尾纳入命令关闭等待，不在存储期间持有调度锁。
func (s *Coordinator) CreateSession(ctx context.Context, input sessionservice.CreateInput) (sessionservice.CreateResult, error) {
	if err := s.enter(); err != nil {
		return sessionservice.CreateResult{}, err
	}
	defer s.running.Done()
	return s.sessions.CreateSession(ctx, input)
}

// SelectSession 加载会话并登记页面使用期；写入绑定仍由首次提交输入创建。
func (s *Coordinator) SelectSession(ctx context.Context, id identity.SessionID, branchID identity.BranchID, bindingID, pageID string) (BindingView, error) {
	if err := s.enter(); err != nil {
		return BindingView{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if bindingID != "" {
		binding := s.bindings[bindingID]
		if binding == nil || binding.sessionID != id || binding.closing || branchID != "" && binding.branchID != branchID {
			return BindingView{}, errorcode.ErrRequestConflict
		}
		branchID = binding.branchID
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
	s.pages[pageID] = id
	return view, nil
}

func (s *Coordinator) CreateBranch(ctx context.Context, id identity.SessionID, branchID identity.BranchID, bindingID string, input sessionmodel.BranchInput) (BindingView, error) {
	if err := s.enter(); err != nil {
		return BindingView{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	requestKey := id.String() + ":" + branchID.String() + ":" + input.RequestID
	if result := s.branchResults[requestKey]; result != "" {
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
	if binding != nil {
		s.releaseBinding(binding)
	}
	s.branchResults[requestKey] = branch.ID.String()
	return s.sessionView(ctx, id, branch.ID, "")
}

func (s *Coordinator) SwitchBranch(ctx context.Context, id identity.SessionID, branchID identity.BranchID, bindingID, name string) (BindingView, error) {
	if err := s.enter(); err != nil {
		return BindingView{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
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
			s.releaseBinding(binding)
		}
		return view, nil
	}
	return BindingView{}, errorcode.ErrNotFound
}

func (s *Coordinator) ListBranches(ctx context.Context, id identity.SessionID, bindingID string) ([]BranchView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
		owner := s.branchOwners[branchKey{id, branch.ID}]
		result = append(result, BranchView{
			Branch:   branch,
			Occupied: owner != "" && owner != bindingID,
			Summary:  summary,
		})
	}
	return result, nil
}

func (s *Coordinator) Tree(ctx context.Context, id identity.SessionID) ([]sessionmodel.TreeNode, error) {
	return s.sessions.Tree(ctx, id)
}

func (s *Coordinator) HistoryAt(ctx context.Context, id identity.SessionID, head string) ([]sessionentry.ContextEntry, error) {
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
		if binding := s.bindings[bindingID]; binding != nil {
			view.Tasks = binding.queue.List(0)
		}
		return view, nil
	}
	return BindingView{}, errorcode.ErrNotFound
}

// bindSubmission 在真正写入前取得分支所有权；另一个页面已有写入绑定时才分叉。
// 调用方持有 Coordinator.mu，使占用判断、分叉和写入权登记连续完成。
func (s *Coordinator) bindSubmission(ctx context.Context, input sessionservice.TaskInput) (*sessionBinding, error) {
	view, err := s.sessionView(ctx, input.SessionID, input.BranchID, "")
	if err != nil {
		return nil, err
	}
	branchID := input.BranchID
	if s.branchOwners[branchKey{input.SessionID, branchID}] != "" {
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
	s.bindings[binding.id] = binding
	s.branchOwners[branchKey{binding.sessionID, binding.branchID}] = binding.id
	return binding, nil
}

func (s *Coordinator) releaseBinding(binding *sessionBinding) {
	delete(s.branchOwners, branchKey{binding.sessionID, binding.branchID})
	delete(s.bindings, binding.id)
	binding.queue.Clear()
}

func (s *Coordinator) navigationBinding(id identity.SessionID, branchID identity.BranchID, bindingID string) (*sessionBinding, error) {
	if bindingID == "" {
		return nil, nil
	}
	binding := s.bindings[bindingID]
	if binding == nil || binding.sessionID != id || binding.branchID != branchID || binding.closing {
		return nil, errorcode.ErrRequestConflict
	}
	if s.executions[executionKey{identity.AgentID(binding.sessionID), binding.branchID}] != nil || len(binding.queue.List(0)) > 0 {
		return nil, agentmodel.ErrExecuting
	}
	return binding, nil
}

// CloseSession 等待本页执行收尾；最后一个页面关闭时才释放共享会话数据。
func (s *Coordinator) CloseSession(ctx context.Context, pageID, bindingID string) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	s.mu.Lock()
	id, opened := s.pages[pageID]
	if !opened {
		s.mu.Unlock()
		return nil
	}
	binding := s.bindings[bindingID]
	var notification coreevent.Event
	if binding != nil {
		if binding.sessionID != id {
			s.mu.Unlock()
			return errorcode.ErrRequestConflict
		}
		binding.closing = true
		for _, active := range s.executions {
			if active.bindingID == bindingID {
				active.cancel(context.Canceled)
			}
		}
		binding.queue.Clear()
		notification = taskQueueEvent(binding)
	}
	s.mu.Unlock()
	// 关闭失败时页面仍然保留，队列清空也必须同步到界面。
	if binding != nil {
		s.loop.Emitter.Emit(notification)
	}
	tick := time.Tick(20 * time.Millisecond)
	for {
		s.mu.Lock()
		busy := false
		for _, active := range s.executions {
			if binding != nil && active.bindingID == bindingID {
				busy = true
				break
			}
		}
		if !busy {
			shared := false
			for otherPage, sessionID := range s.pages {
				if otherPage != pageID && sessionID == id {
					shared = true
					break
				}
			}
			if !shared {
				if err := s.sessions.ReleaseSession(ctx, id); err != nil {
					if binding != nil {
						binding.closing = false
					}
					s.mu.Unlock()
					return err
				}
				maps.DeleteFunc(s.branchResults, func(key, _ string) bool { return strings.HasPrefix(key, id.String()+":") })
			}
			if binding != nil {
				s.releaseBinding(binding)
			}
			delete(s.pages, pageID)
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick:
		}
	}
}
