package jsonl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	projectmodel "praxis/internal/core/project"
	securitymodel "praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	turnmodel "praxis/internal/core/turn"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/repository"
)

// Repositories 将同一个日志事务入口提供给所有业务仓储。
type Repositories struct {
	Projects          ProjectRepository
	Workspaces        WorkspaceRepository
	Sessions          SessionRepository
	Agents            AgentRepository
	Tasks             TaskRepository
	SecuritySnapshots SecuritySnapshotRepository
	Policies          PolicyRepository
	Contexts          ContextRepository
	ToolInvocations   ToolRepository
	Controls          ControlRepository
	Turns             TurnRepository
	Messages          MessageStreams
}

// Repositories 返回共享提交边界与投影的仓储集合。
func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:          ProjectRepository{s},
		Workspaces:        WorkspaceRepository{s},
		Sessions:          SessionRepository{s},
		Agents:            AgentRepository{s},
		Tasks:             TaskRepository{s},
		SecuritySnapshots: SecuritySnapshotRepository{s},
		Policies:          PolicyRepository{s},
		Contexts:          ContextRepository{s},
		ToolInvocations:   ToolRepository{s},
		Controls:          ControlRepository{s},
		Turns:             TurnRepository{s},
		Messages:          MessageStreams{s},
	}
}

type ProjectRepository struct{ s *Store }
type WorkspaceRepository struct{ s *Store }
type SessionRepository struct{ s *Store }
type AgentRepository struct{ s *Store }
type TaskRepository struct{ s *Store }
type SecuritySnapshotRepository struct{ s *Store }
type PolicyRepository struct{ s *Store }
type ContextRepository struct{ s *Store }
type ToolRepository struct{ s *Store }
type ControlRepository struct{ s *Store }
type TurnRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r TurnRepository) Get(ctx context.Context, id contracts.TurnID) (turnmodel.Turn, error) {
	return get[turnmodel.Turn](ctx, r.s, "turn", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r TurnRepository) Save(ctx context.Context, v turnmodel.Turn) error {
	return save(ctx, r.s, v.SessionID.String(), "turn", v.ID.String(), "turn.saved", v)
}

// ListOpen 返回全部尚未结算的迭代，供启动恢复发现待收敛的 Session。
func (r TurnRepository) ListOpen(ctx context.Context) ([]turnmodel.Turn, error) {
	v, e := list[turnmodel.Turn](ctx, r.s, "turn", func(v turnmodel.Turn) bool {
		return v.Status == turnmodel.TurnRunning
	})
	return sorted(v, func(a, b turnmodel.Turn) int {
		return cmp.Or(
			cmp.Compare(a.SessionID, b.SessionID),
			cmp.Compare(a.TaskID, b.TaskID),
			cmp.Compare(a.Sequence, b.Sequence),
		)
	}), e
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r ProjectRepository) Get(ctx context.Context, id contracts.ProjectID) (projectmodel.Project, error) {
	return get[projectmodel.Project](ctx, r.s, "project", id.String())
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r ProjectRepository) Save(ctx context.Context, v projectmodel.Project) error {
	return save(ctx, r.s, "", "project", v.ID.String(), "project.saved", v)
}

// List 返回查询结果的独立副本，非正数 limit 默认 100 条。
func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	v, e := list[projectmodel.Project](ctx, r.s, "project", nil)
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

// ListByProject 返回指定 Project 的对象副本，非正数 limit 默认 100 条。
func (r WorkspaceRepository) ListByProject(ctx context.Context, id contracts.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	v, e := list[workspacemodel.Workspace](ctx, r.s, "workspace", func(v workspacemodel.Workspace) bool { return v.ProjectID == id })
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

// List 返回查询结果的独立副本，非正数 limit 默认 100 条。
func (r SessionRepository) List(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", "", limit)
}

// ListByProject 返回指定 Project 的对象副本，非正数 limit 默认 100 条。
func (r SessionRepository) ListByProject(ctx context.Context, id contracts.ProjectID, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, id, "", limit)
}

// SearchTitle 在内存目录中按标题子串查询，空字符串返回完整目录。
func (r SessionRepository) SearchTitle(ctx context.Context, title string, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", strings.ToLower(strings.TrimSpace(title)), limit)
}
func (r SessionRepository) find(ctx context.Context, project contracts.ProjectID, title string, limit int) ([]sessionmodel.Session, error) {
	v, e := list[sessionmodel.Session](ctx, r.s, "session", func(v sessionmodel.Session) bool {
		return (project == "" || v.ProjectID == project) && strings.Contains(strings.ToLower(v.Title), title)
	})
	return capped(sorted(v, func(a, b sessionmodel.Session) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), e
}

// HasMessages 判断会话下是否已有任何消息；会话流与各 Agent 私有流共用同一 scope，因此一次投影扫描即可判定。
func (r SessionRepository) HasMessages(ctx context.Context, id contracts.SessionID) (bool, error) {
	found := false
	err := r.s.view(ctx, func(objects map[objectKey]object) error {
		for key, value := range objects {
			if key.collection == "message" && value.scope == id.String() {
				found = true
				return nil
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
		return contracts.InvalidValue("title", "标题必须是规范化非空字符串")
	}
	v.Title = title
	v.UpdatedAt = at
	return r.Save(ctx, v)
}

// Delete 追加删除标记并清除关联投影；仍有活动 Task 时拒绝删除。
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
	v, e := list[agentmodel.Agent](ctx, r.s, "agent", func(v agentmodel.Agent) bool { return v.SessionID == id })
	return capped(sorted(v, func(a, b agentmodel.Agent) int { return cmp.Compare(a.ID, b.ID) }), limit), e
}

// GetBySessionAndDefinition 按 Session 与 Agent 定义查找，未找到时返回 contracts.ErrNotFound。
func (r AgentRepository) GetBySessionAndDefinition(ctx context.Context, id contracts.SessionID, definition contracts.AgentDefinitionID) (agentmodel.Agent, error) {
	v, e := list[agentmodel.Agent](ctx, r.s, "agent", func(v agentmodel.Agent) bool { return v.SessionID == id && v.DefinitionID == definition })
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
	v, e := list[taskmodel.Task](ctx, r.s, "task", func(v taskmodel.Task) bool { return v.AgentID == id && v.RequestID == request })
	if e != nil {
		return taskmodel.Task{}, e
	}
	if len(v) == 0 {
		return taskmodel.Task{}, contracts.ErrNotFound
	}
	return v[0], nil
}

// GetActiveByAgent 读取 Agent 唯一的活动 Task，未找到时返回 contracts.ErrNotFound。
func (r TaskRepository) GetActiveByAgent(ctx context.Context, id contracts.AgentID) (taskmodel.Task, error) {
	v, e := list[taskmodel.Task](ctx, r.s, "task", func(v taskmodel.Task) bool { return v.AgentID == id && v.Active() })
	if e != nil {
		return taskmodel.Task{}, e
	}
	if len(v) == 0 {
		return taskmodel.Task{}, contracts.ErrNotFound
	}
	return v[0], nil
}

// ListActive 按创建时间返回所有活动 Task，供启动恢复使用。
func (r TaskRepository) ListActive(ctx context.Context) ([]taskmodel.Task, error) {
	v, e := list[taskmodel.Task](ctx, r.s, "task", func(v taskmodel.Task) bool { return v.Active() })
	return sorted(v, func(a, b taskmodel.Task) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

// ListByAgent 返回指定 Agent 的对象副本。
func (r TaskRepository) ListByAgent(ctx context.Context, id contracts.AgentID, limit int) ([]taskmodel.Task, error) {
	v, e := list[taskmodel.Task](ctx, r.s, "task", func(v taskmodel.Task) bool { return v.AgentID == id })
	return capped(sorted(v, func(a, b taskmodel.Task) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	}), limit), e
}

// CountActiveBySession 返回 Session 内活动 Task 的数量。
func (r TaskRepository) CountActiveBySession(ctx context.Context, id contracts.SessionID) (int, error) {
	v, e := list[taskmodel.Task](ctx, r.s, "task", func(v taskmodel.Task) bool { return v.SessionID == id && v.Active() })
	return len(v), e
}

// Get 读取指定对象，不存在时返回 contracts.ErrNotFound。
func (r SecuritySnapshotRepository) Get(ctx context.Context, id contracts.TaskID) (contracts.SecuritySnapshot, error) {
	v, e := (TaskRepository{r.s}).Get(ctx, id)
	return v.Input.Security, e
}

type policyRecord struct {
	AgentID contracts.AgentID
	Policy  securitymodel.AgentSecurityPolicy
}

func policyKey(id contracts.AgentID, revision uint64) string {
	return fmt.Sprintf("%s:%d", id, revision)
}

// Save 校验并保存对象，事务未提交时只更新工作区。
func (r PolicyRepository) Save(ctx context.Context, id contracts.AgentID, v securitymodel.AgentSecurityPolicy) error {
	if e := v.Validate(); e != nil {
		return e
	}
	agent, e := (AgentRepository{r.s}).Get(ctx, id)
	if e != nil {
		return e
	}
	return save(ctx, r.s, agent.SessionID.String(), "policy", policyKey(id, v.Revision), "policy.saved", policyRecord{id, v})
}

// GetCurrent 返回 Agent 的最新策略版本，未找到时返回 contracts.ErrNotFound。
func (r PolicyRepository) GetCurrent(ctx context.Context, id contracts.AgentID) (securitymodel.AgentSecurityPolicy, error) {
	values, e := list[policyRecord](ctx, r.s, "policy", func(v policyRecord) bool { return v.AgentID == id })
	if e != nil {
		return securitymodel.AgentSecurityPolicy{}, e
	}
	if len(values) == 0 {
		return securitymodel.AgentSecurityPolicy{}, contracts.ErrNotFound
	}
	sorted(values, func(a, b policyRecord) int { return cmp.Compare(b.Policy.Revision, a.Policy.Revision) })
	return values[0].Policy, nil
}

// GetByRevision 读取指定策略版本，第二个返回值表示是否存在。
func (r PolicyRepository) GetByRevision(ctx context.Context, id contracts.AgentID, revision uint64) (securitymodel.AgentSecurityPolicy, bool, error) {
	v, e := get[policyRecord](ctx, r.s, "policy", policyKey(id, revision))
	if errors.Is(e, contracts.ErrNotFound) {
		return v.Policy, false, nil
	}
	return v.Policy, e == nil, e
}

// Append 幂等追加记录，身份相同但内容不一致时返回冲突。
func (r ContextRepository) Append(ctx context.Context, v contextmodel.SessionContextEntry) error {
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
func (r ContextRepository) GetByID(ctx context.Context, id contracts.ContextEntryID) (contextmodel.SessionContextEntry, bool, error) {
	v, e := get[contextmodel.SessionContextEntry](ctx, r.s, "context", id.String())
	if errors.Is(e, contracts.ErrNotFound) {
		return v, false, nil
	}
	return v, e == nil, e
}

// List 返回查询结果的独立副本，非正数 limit 默认 100 条。
func (r ContextRepository) List(ctx context.Context, id contracts.SessionID, after string, limit int) ([]contextmodel.SessionContextEntry, error) {
	v, e := list[contextmodel.SessionContextEntry](ctx, r.s, "context", func(v contextmodel.SessionContextEntry) bool { return v.SessionID == id })
	sorted(v, func(a, b contextmodel.SessionContextEntry) int {
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
	v, e := list[toolmodel.ToolInvocation](ctx, r.s, "tool", func(v toolmodel.ToolInvocation) bool { return v.TaskID == id && v.ProviderToolCallID == call })
	if e != nil {
		return toolmodel.ToolInvocation{}, e
	}
	if len(v) == 0 {
		return toolmodel.ToolInvocation{}, contracts.ErrNotFound
	}
	return v[0], nil
}

// ListUnsettledBySession 返回指定 Session 尚未结算的工具调用，供恢复使用。
func (r ToolRepository) ListUnsettledBySession(ctx context.Context, id contracts.SessionID) ([]toolmodel.ToolInvocation, error) {
	v, e := list[toolmodel.ToolInvocation](ctx, r.s, "tool", func(v toolmodel.ToolInvocation) bool { return v.SessionID == id && !v.Status.Terminal() })
	return sorted(v, func(a, b toolmodel.ToolInvocation) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

// NextSequence 返回指定 Agent 的下一队列序号，必须在入队事务中调用。
func (r TaskRepository) NextSequence(ctx context.Context, id contracts.AgentID) (uint64, error) {
	v, e := list[taskmodel.Task](ctx, r.s, "task", func(v taskmodel.Task) bool { return v.AgentID == id })
	var seq uint64
	for _, item := range v {
		seq = max(seq, item.Sequence)
	}
	return seq + 1, e
}

// FindNextPendingByAgent 按 FIFO 顺序返回待执行 Task，空队列返回 ErrNotFound。
func (r TaskRepository) FindNextPendingByAgent(ctx context.Context, id contracts.AgentID) (taskmodel.Task, error) {
	v, e := list[taskmodel.Task](ctx, r.s, "task", func(v taskmodel.Task) bool {
		return v.AgentID == id && v.Status == taskmodel.TaskPending
	})
	if e != nil {
		return taskmodel.Task{}, e
	}
	if len(v) == 0 {
		return taskmodel.Task{}, contracts.ErrNotFound
	}
	sorted(v, func(a, b taskmodel.Task) int { return cmp.Compare(a.Sequence, b.Sequence) })
	return v[0], nil
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
	v, e := list[agentmodel.AgentControlCommand](ctx, r.s, "control", func(v agentmodel.AgentControlCommand) bool {
		return v.AgentID == id && v.Status == agentmodel.AgentControlPending
	})
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
	_ repository.AgentSecurityPolicyRepository = PolicyRepository{}
	_ repository.SessionContextRepository      = ContextRepository{}
	_ repository.AgentControlCommandRepository = ControlRepository{}
	_ repository.TurnRepository                = TurnRepository{}
)
