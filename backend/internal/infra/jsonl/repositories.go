package jsonl

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/repository"
)

// Repositories 将同一个日志事务入口提供给所有业务仓储。
type Repositories struct {
	Projects        ProjectRepository
	Workspaces      WorkspaceRepository
	Sessions        SessionRepository
	Agents          AgentRepository
	Tasks           TaskRepository
	Contexts        ContextRepository
	ToolInvocations ToolRepository
	Controls        ControlRepository
	Messages        MessageStreams
	ToolExecutions  repository.ToolExecutionRepository
}

// Repositories 返回共享提交边界与位置索引的仓储集合。
func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:        ProjectRepository{s},
		Workspaces:      WorkspaceRepository{s},
		Sessions:        SessionRepository{s},
		Agents:          AgentRepository{s},
		Tasks:           TaskRepository{s},
		Contexts:        ContextRepository{s},
		ToolInvocations: ToolRepository{s},
		Controls:        ControlRepository{s},
		Messages:        MessageStreams{s},
		ToolExecutions:  ToolExecutionRepository{s},
	}
}

type ProjectRepository struct{ s *Store }
type WorkspaceRepository struct{ s *Store }
type SessionRepository struct{ s *Store }
type AgentRepository struct{ s *Store }
type TaskRepository struct{ s *Store }
type ContextRepository struct{ s *Store }
type ToolRepository struct{ s *Store }
type ControlRepository struct{ s *Store }

func (r ProjectRepository) Get(ctx context.Context, id contracts.ProjectID) (projectmodel.Project, error) {
	return get[projectmodel.Project](ctx, r.s, "project", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r ProjectRepository) Save(ctx context.Context, v projectmodel.Project) error {
	return save(ctx, r.s, "", "project", v.ID.String(), "project.saved", v)
}

// List 返回查询结果的独立副本，limit 为 0 时不限制。
func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	v, e := listIndexed[projectmodel.Project](ctx, r.s, collectionKey("project"), nil)
	return capped(sorted(v, func(a, b projectmodel.Project) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), e
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r WorkspaceRepository) Get(ctx context.Context, id contracts.WorkspaceID) (workspacemodel.Workspace, error) {
	return get[workspacemodel.Workspace](ctx, r.s, "workspace", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r WorkspaceRepository) Save(ctx context.Context, v workspacemodel.Workspace) error {
	return save(ctx, r.s, "", "workspace", v.ID.String(), "workspace.saved", v)
}

// ListByProject 返回指定 Project 的对象副本，limit 为 0 时不限制。
func (r WorkspaceRepository) ListByProject(ctx context.Context, id contracts.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	v, e := listIndexed[workspacemodel.Workspace](ctx, r.s, lookupKey{kind: "workspaces-project", owner: id.String()}, nil)
	return capped(sorted(v, func(a, b workspacemodel.Workspace) int { return cmp.Compare(a.ID, b.ID) }), limit), e
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r SessionRepository) Get(ctx context.Context, id contracts.SessionID) (sessionmodel.Session, error) {
	return get[sessionmodel.Session](ctx, r.s, "session", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r SessionRepository) Save(ctx context.Context, v sessionmodel.Session) error {
	return save(ctx, r.s, v.ID.String(), "session", v.ID.String(), "session.saved", v)
}

// List 返回查询结果的独立副本，limit 为 0 时不限制。
func (r SessionRepository) List(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", "", limit)
}

// ListByProject 返回指定 Project 的对象副本，limit 为 0 时不限制。
func (r SessionRepository) ListByProject(ctx context.Context, id contracts.ProjectID, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, id, "", limit)
}

// SearchTitle 按会话目录索引查询标题子串，空字符串返回完整目录。
func (r SessionRepository) SearchTitle(ctx context.Context, title string, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", title, limit)
}
func (r SessionRepository) find(ctx context.Context, project contracts.ProjectID, title string, limit int) ([]sessionmodel.Session, error) {
	group := collectionKey("session")
	if project != "" {
		group = lookupKey{kind: "sessions-project", owner: project.String()}
	}
	v, e := listIndexed[sessionmodel.Session](ctx, r.s, group, func(v sessionmodel.Session) bool {
		return (project == "" || v.ProjectID == project) && strings.Contains(strings.ToLower(v.Title), title)
	})
	return capped(sorted(v, func(a, b sessionmodel.Session) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), e
}

// HasMessages 读取消息存在性元数据，不加载消息正文或位置列表。
func (r SessionRepository) HasMessages(ctx context.Context, id contracts.SessionID) (bool, error) {
	found := false
	err := r.s.view(ctx, func(readCtx context.Context) error {
		if _, err := r.Get(readCtx, id); err != nil {
			return err
		}
		found = r.s.index.hasMessages[id.String()]
		for key, change := range r.s.tx(readCtx).changes {
			if key.collection == "message" && change.entry.location.Scope == id.String() {
				found = true
				break
			}
		}
		return nil
	})
	return found, err
}

// Rename 更新规范化的非空标题，归属和创建时间保持不变。
func (r SessionRepository) Rename(ctx context.Context, id contracts.SessionID, title string, at time.Time) error {
	v, e := r.Get(ctx, id)
	if e != nil {
		return e
	}
	if title == "" || title != strings.TrimSpace(title) {
		return contracts.InvalidValue("title", "title must be a non-empty canonical string")
	}
	v.Title = title
	v.UpdatedAt = at
	return r.Save(ctx, v)
}

// Delete 追加删除标记并清理所属 Session 的查询索引；仍有活动 Task 时拒绝删除。
func (r SessionRepository) Delete(ctx context.Context, id contracts.SessionID) ([]string, error) {
	if _, e := r.Get(ctx, id); e != nil {
		return nil, e
	}
	n, e := (TaskRepository{r.s}).CountActiveBySession(ctx, id)
	if e != nil {
		return nil, e
	}
	if n > 0 {
		return nil, contracts.ErrAgentExecuting
	}
	return nil, r.s.change(ctx, id.String(), "session", id.String(), "session.deleted", nil)
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r AgentRepository) Get(ctx context.Context, id contracts.AgentID) (agentmodel.Agent, error) {
	return get[agentmodel.Agent](ctx, r.s, "agent", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r AgentRepository) Save(ctx context.Context, v agentmodel.Agent) error {
	return save(ctx, r.s, v.SessionID.String(), "agent", v.ID.String(), "agent.saved", v)
}

// ListBySession 返回指定 Session 的对象副本。
func (r AgentRepository) ListBySession(ctx context.Context, id contracts.SessionID, limit int) ([]agentmodel.Agent, error) {
	v, e := listIndexed[agentmodel.Agent](ctx, r.s, lookupKey{kind: "agents-session", owner: id.String()}, nil)
	return capped(sorted(v, func(a, b agentmodel.Agent) int { return cmp.Compare(a.ID, b.ID) }), limit), e
}

// GetBySessionAndDefinition 按 Session 与 Agent 定义查找，未找到时返回 contracts.ErrNotFound。
func (r AgentRepository) GetBySessionAndDefinition(ctx context.Context, id contracts.SessionID, definition contracts.AgentDefinitionID) (agentmodel.Agent, error) {
	v, e := listIndexed[agentmodel.Agent](ctx, r.s, lookupKey{kind: "agents-definition", owner: id.String(), value: definition.String()}, nil)
	if e != nil {
		return agentmodel.Agent{}, e
	}
	if len(v) == 0 {
		return agentmodel.Agent{}, contracts.ErrNotFound
	}
	sorted(v, func(a, b agentmodel.Agent) int { return cmp.Compare(a.ID, b.ID) })
	return v[0], nil
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r TaskRepository) Get(ctx context.Context, id contracts.TaskID) (taskmodel.Task, error) {
	return get[taskmodel.Task](ctx, r.s, "task", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r TaskRepository) Save(ctx context.Context, v taskmodel.Task) error {
	return save(ctx, r.s, v.SessionID.String(), "task", v.ID.String(), "task.saved", v)
}

// FindByRequest 按 Agent 与请求身份查找，未找到时返回 contracts.ErrNotFound。
func (r TaskRepository) FindByRequest(ctx context.Context, id contracts.AgentID, request contracts.RequestID) (taskmodel.Task, error) {
	return getByClaim[taskmodel.Task](ctx, r.s, lookupKey{kind: "task-request", owner: id.String(), value: request.String()})
}

// GetActiveByAgent 通过唯一索引读取 Agent 的活动 Task。
func (r TaskRepository) GetActiveByAgent(ctx context.Context, id contracts.AgentID) (taskmodel.Task, error) {
	return getByClaim[taskmodel.Task](ctx, r.s, lookupKey{kind: "active-agent", owner: id.String()})
}

// ListActive 按创建时间返回所有活动 Task，供启动恢复使用。
func (r TaskRepository) ListActive(ctx context.Context) ([]taskmodel.Task, error) {
	v, e := listIndexed[taskmodel.Task](ctx, r.s, lookupKey{kind: "active-tasks"}, nil)
	return sorted(v, func(a, b taskmodel.Task) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

// ListByAgent 返回指定 Agent 的对象副本；非正数 limit 返回全部任务。
func (r TaskRepository) ListByAgent(ctx context.Context, id contracts.AgentID, limit int) ([]taskmodel.Task, error) {
	v, e := listIndexed[taskmodel.Task](ctx, r.s, lookupKey{kind: "tasks-agent", owner: id.String()}, nil)
	sorted(v, func(a, b taskmodel.Task) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	if limit > 0 {
		v = capped(v, limit)
	}
	return v, e
}

// CountActiveBySession 返回 Session 内活动 Task 的数量。
func (r TaskRepository) CountActiveBySession(ctx context.Context, id contracts.SessionID) (int, error) {
	count := 0
	err := r.s.view(ctx, func(readCtx context.Context) error {
		count = len(r.s.keys(readCtx, lookupKey{kind: "active-session", owner: id.String()}))
		return nil
	})
	return count, err
}

// Append 幂等追加记录，身份相同但内容不一致时返回冲突。
func (r ContextRepository) Append(ctx context.Context, v sessionmodel.SessionContextEntry) error {
	existing, ok, e := r.GetByID(ctx, v.ID)
	if e != nil {
		return e
	}
	if ok {
		v.CreatedAt = existing.CreatedAt
	}
	return save(ctx, r.s, v.SessionID.String(), "context", v.ID.String(), "context.entry_appended", v)
}

// GetByID 读取指定记录，第二个返回值表示是否存在。
func (r ContextRepository) GetByID(ctx context.Context, id contracts.ContextEntryID) (sessionmodel.SessionContextEntry, bool, error) {
	v, e := get[sessionmodel.SessionContextEntry](ctx, r.s, "context", id.String())
	if errors.Is(e, contracts.ErrNotFound) {
		return v, false, nil
	}
	return v, e == nil, e
}

// List 返回查询结果的独立副本，limit 为 0 时不限制。
func (r ContextRepository) List(ctx context.Context, id contracts.SessionID, after string, limit int) ([]sessionmodel.SessionContextEntry, error) {
	v, e := listIndexed[sessionmodel.SessionContextEntry](ctx, r.s, lookupKey{kind: "contexts-session", owner: id.String()}, nil)
	sorted(v, func(a, b sessionmodel.SessionContextEntry) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	if after != "" {
		found := false
		for i, item := range v {
			if item.ID.String() == after {
				v = v[i+1:]
				found = true
				break
			}
		}
		if !found {
			v = nil
		}
	}
	return capped(v, limit), e
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r ToolRepository) Get(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error) {
	return get[toolmodel.ToolInvocation](ctx, r.s, "tool", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r ToolRepository) Save(ctx context.Context, v toolmodel.ToolInvocation) error {
	return save(ctx, r.s, v.SessionID.String(), "tool", v.ID.String(), "tool.saved", v)
}

// FindByTaskCall 按 Task 和 Provider 调用身份查询，第二个返回值表示是否存在。
func (r ToolRepository) FindByTaskCall(ctx context.Context, id contracts.TaskID, call string) (toolmodel.ToolInvocation, error) {
	return getByClaim[toolmodel.ToolInvocation](ctx, r.s, lookupKey{kind: "tool-call", owner: id.String(), value: call})
}

// ListUnsettledBySession 返回指定 Session 尚未结算的工具调用，供恢复使用。
func (r ToolRepository) ListUnsettledBySession(ctx context.Context, id contracts.SessionID) ([]toolmodel.ToolInvocation, error) {
	v, e := listIndexed[toolmodel.ToolInvocation](ctx, r.s, lookupKey{kind: "unsettled-session", owner: id.String()}, nil)
	return sorted(v, func(a, b toolmodel.ToolInvocation) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

// NextSequence 返回指定 Agent 的下一队列序号，必须在入队事务中调用。
func (r TaskRepository) NextSequence(ctx context.Context, id contracts.AgentID) (uint64, error) {
	var sequence uint64
	err := r.s.view(ctx, func(readCtx context.Context) error {
		sequence = r.s.index.maxQueue[id.String()]
		for _, change := range r.s.tx(readCtx).changes {
			if slices.Contains(change.entry.groups, lookupKey{kind: "tasks-agent", owner: id.String()}) {
				sequence = max(sequence, change.entry.sequence)
			}
		}
		return nil
	})
	return sequence + 1, err
}

// FindNextPendingByAgent 仅访问该 Agent 的 Pending 索引，返回 FIFO 队首。
func (r TaskRepository) FindNextPendingByAgent(ctx context.Context, id contracts.AgentID) (taskmodel.Task, error) {
	var result taskmodel.Task
	err := r.s.view(ctx, func(readCtx context.Context) error {
		var next entityKey
		var sequence uint64
		for _, key := range r.s.keys(readCtx, lookupKey{kind: "pending-agent", owner: id.String()}) {
			entry, _ := r.s.entry(readCtx, key)
			if next.id == "" || entry.sequence < sequence {
				next, sequence = key, entry.sequence
			}
		}
		if next.id == "" {
			return contracts.ErrNotFound
		}
		var err error
		result, err = r.Get(readCtx, contracts.TaskID(next.id))
		return err
	})
	return result, err
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r ControlRepository) Get(ctx context.Context, id contracts.AgentControlCommandID) (agentmodel.AgentControlCommand, error) {
	return get[agentmodel.AgentControlCommand](ctx, r.s, "control", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r ControlRepository) Save(ctx context.Context, v agentmodel.AgentControlCommand) error {
	a, e := (AgentRepository{r.s}).Get(ctx, v.AgentID)
	if e != nil {
		return e
	}
	return save(ctx, r.s, a.SessionID.String(), "control", v.ID.String(), "control.saved", v)
}

// ListOpenByAgent 返回 Agent 尚未应用的控制命令。
func (r ControlRepository) ListOpenByAgent(ctx context.Context, id contracts.AgentID, limit int) ([]agentmodel.AgentControlCommand, error) {
	v, e := listIndexed[agentmodel.AgentControlCommand](ctx, r.s, lookupKey{kind: "controls-agent", owner: id.String()}, nil)
	return capped(sorted(v, func(a, b agentmodel.AgentControlCommand) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), e
}

var (
	_ repository.TxRunner                      = (*Store)(nil)
	_ repository.ProjectRepository             = ProjectRepository{}
	_ repository.WorkspaceRepository           = WorkspaceRepository{}
	_ repository.SessionRepository             = SessionRepository{}
	_ repository.SessionAgentRepository        = AgentRepository{}
	_ repository.TaskRepository                = TaskRepository{}
	_ repository.ToolInvocationRepository      = ToolRepository{}
	_ repository.SessionContextRepository      = ContextRepository{}
	_ repository.AgentControlCommandRepository = ControlRepository{}
)

func (r ToolRepository) ListByTask(ctx context.Context, id contracts.TaskID) ([]toolmodel.ToolInvocation, error) {
	return listIndexed[toolmodel.ToolInvocation](ctx, r.s, lookupKey{kind: "tools-task", owner: id.String()}, nil)
}
func (r ToolRepository) ListAwaitingByAgent(ctx context.Context, id contracts.AgentID) ([]toolmodel.ToolInvocation, error) {
	values, err := listIndexed[toolmodel.ToolInvocation](ctx, r.s, lookupKey{kind: "awaiting-agent", owner: id.String()}, nil)
	return sorted(values, func(a, b toolmodel.ToolInvocation) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), err
}
