package runtime_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	securitymodel "praxis/internal/core/security"
	taskmodel "praxis/internal/core/task"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/hooks"
	"praxis/internal/infra/jsonl"
	"praxis/internal/repository"
	tasklifecycle "praxis/internal/service/runtime/task/lifecycle"
	taskstart "praxis/internal/service/runtime/task/start"
	applicationsession "praxis/internal/service/session"
)

var errRollback = errors.New("模拟提交前回滚")

// rollbackTx 在业务回调成功后注入失败，并观察 Hook 的 Context 是否传入事务。
type rollbackTx struct {
	repository.TxRunner
	fail      bool
	operation hooks.OperationKind
}

func (tx *rollbackTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	tx.operation, _ = ctx.Value(lifecycleOperationKey{}).(hooks.OperationKind)
	return tx.TxRunner.InTx(ctx, func(txCtx context.Context) error {
		if err := fn(txCtx); err != nil {
			return err
		}
		if tx.fail {
			return errRollback
		}
		return nil
	})
}

type lifecycleModels struct{}

func (lifecycleModels) Definition(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return agentmodel.AgentDefinition{
		ID:           agentmodel.DefinitionPrimary,
		Profile:      contracts.ProfilePrimary,
		Instructions: "测试指令",
		Revision:     "revision",
	}, nil
}
func (lifecycleModels) FreezeTaskModel(string, string, string) (model.ModelSnapshot, error) {
	return model.ModelSnapshot{
		ProviderID:      "provider",
		ModelID:         "model",
		APIFormat:       "openai_chat",
		ContextWindow:   4096,
		MaxOutputTokens: 100,
		BaseURL:         "https://example.invalid",
	}, nil
}
func (m lifecycleModels) FreezeDefaultTaskModel(contracts.AgentDefinitionID) (model.ModelSnapshot, error) {
	return m.FreezeTaskModel("", "", "")
}

type lifecycleOperationKey struct{}

type lifecycleOperationFunc func(context.Context, hooks.Operation) (context.Context, hooks.Scope)

func (f lifecycleOperationFunc) Start(ctx context.Context, op hooks.Operation) (context.Context, hooks.Scope) {
	return f(ctx, op)
}

type lifecycleScope struct {
	end func(hooks.Result)
}

func (s lifecycleScope) OnSignal(hooks.Signal)   {}
func (s lifecycleScope) End(result hooks.Result) { s.end(result) }

type lifecycleAttempt struct {
	operation hooks.Operation
	result    hooks.Result
	ends      int
}

// unusedTextReader 不参与本测试；保留窄 Port，不引入文件系统读取。
type unusedTextReader struct {
	applicationsession.WorkspaceTextReader
}

func TestLifecycleOperationHooks(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, err := jsonl.Open(ctx, filepath.Join(root, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	repos := store.Repositories()
	tx := &rollbackTx{TxRunner: store}
	at := time.Now().UTC()
	project, err := projectmodel.NewProject("project", "测试", root, "workspace", at)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspacemodel.NewWorkspace("workspace", project.ID, workspacemodel.WorkspaceProjectRoot, root, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InTx(ctx, func(txCtx context.Context) error {
		if err := repos.Projects.Save(txCtx, project); err != nil {
			return err
		}
		return repos.Workspaces.Save(txCtx, workspace)
	}); err != nil {
		t.Fatal(err)
	}
	params := applicationsession.CreateParams{
		SessionID:   "session",
		AgentID:     "agent",
		ProjectID:   "project",
		WorkspaceID: "workspace",
	}
	var sessions *applicationsession.Service
	var attempts []*lifecycleAttempt
	var boundaries []string
	reenteredEnd := false
	observer := lifecycleOperationFunc(func(ctx context.Context, op hooks.Operation) (context.Context, hooks.Scope) {
		if op.SessionID != "session" || op.AgentID != "agent" || op.Kind == hooks.OperationTaskSettle && op.TaskID == "" {
			t.Fatalf("生命周期操作归属不完整：%+v", op)
		}
		// 使用事务外 Context 回读，验证 Start 没有持有仓储事务锁。
		if _, err := repos.Sessions.List(t.Context(), 10); err != nil {
			t.Fatal(err)
		}
		attempt := &lifecycleAttempt{operation: op}
		attempts = append(attempts, attempt)
		boundaries = append(boundaries, "start:"+string(op.Kind))
		return context.WithValue(ctx, lifecycleOperationKey{}, op.Kind), lifecycleScope{end: func(result hooks.Result) {
			attempt.result = result
			attempt.ends++
			if result.Err != nil && result.Outcome != "" {
				t.Fatal("回滚的结算暴露了未提交 Outcome")
			}
			if op.Kind == hooks.OperationSessionCreate {
				if !reenteredEnd {
					// 第一次创建会回滚；重入验证 End 已退出 Session 的创建锁。
					reenteredEnd = true
					if _, err := sessions.CreateSession(t.Context(), params); !errors.Is(err, errRollback) {
						t.Fatal("Session End 重入失败：", err)
					}
				}
				if result.Err == nil {
					if _, err := repos.Sessions.Get(t.Context(), op.SessionID); err != nil {
						t.Fatal("Session End 早于事务提交：", err)
					}
				}
			}
			if op.Kind == hooks.OperationTaskSettle && result.Err == nil {
				task, err := repos.Tasks.Get(t.Context(), op.TaskID)
				if err != nil || task.Status != taskmodel.TaskEnded || task.Outcome != result.Outcome {
					t.Fatalf("End 没有观察到持久化仲裁结果：%+v %v", result, err)
				}
			}
			boundaries = append(boundaries, "end:"+string(op.Kind))
		}}
	})
	d := hooks.NewDispatcher(observer)
	sessions, err = applicationsession.NewService(applicationsession.Config{
		Transactions:    tx,
		HookDispatcher:  d,
		Projects:        repos.Projects,
		Workspaces:      repos.Workspaces,
		Sessions:        repos.Sessions,
		Contexts:        repos.Contexts,
		Policies:        repos.Policies,
		Agents:          repos.Agents,
		Tasks:           repos.Tasks,
		ToolInvocations: repos.ToolInvocations,
		Controls:        repos.Controls,
		Turns:           repos.Turns,
		Definitions:     (lifecycleModels{}).Definition,
		PolicyFactory: func(workspacemodel.Workspace, contracts.AgentDefinitionID) (securitymodel.AgentSecurityPolicy, error) {
			return securitymodel.NewAgentSecurityPolicy(1, securitymodel.CapabilityPolicy{}, securitymodel.SandboxPolicy{Mode: contracts.SandboxReadOnly}, securitymodel.ApprovalPolicy{Mode: contracts.ApprovalAlwaysAsk})
		},
		Messages:     repos.Messages,
		UsageRecords: func(context.Context, string) ([]model.ModelUsageRecord, error) { return nil, nil },
		TextReader:   unusedTextReader{},
	})
	if err != nil {
		t.Fatal(err)
	}
	tx.fail = true
	if _, err := sessions.CreateSession(ctx, params); !errors.Is(err, errRollback) {
		t.Fatal("Session 回滚错误丢失：", err)
	}
	if _, err := repos.Sessions.Get(ctx, params.SessionID); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatal("Session 回滚后仍存在：", err)
	}
	tx.fail = false
	for range 2 {
		if _, err := sessions.CreateSession(ctx, params); err != nil {
			t.Fatal(err)
		}
	}
	if tx.operation != hooks.OperationSessionCreate {
		t.Fatal("Session Hook 的 Context 未传入事务")
	}
	start, err := taskstart.NewService(taskstart.Config{
		Transactions:        tx,
		Workspaces:          repos.Workspaces,
		Sessions:            repos.Sessions,
		Policies:            repos.Policies,
		Agents:              repos.Agents,
		Tasks:               repos.Tasks,
		Executions:          &agentmodel.Executions{},
		Messages:            repos.Messages,
		PrimaryAgent:        sessions,
		Definitions:         lifecycleModels{},
		AgentDefinitionsDir: filepath.Join(root, "agents"),
		Models:              lifecycleModels{},
		ContextProvider:     sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := taskstart.SendInputParams{
		SessionID: "session",
		AgentID:   "agent",
		RequestID: "request",
		Content:   "你好",
	}
	started, err := start.SendInput(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var terminalEvents []tasklifecycle.TerminalEvent
	lifecycle, err := tasklifecycle.NewService(tasklifecycle.Config{
		Transactions:   tx,
		HookDispatcher: d,
		Sessions:       repos.Sessions,
		Agents:         repos.Agents,
		Tasks:          repos.Tasks,
		Controls:       repos.Controls,
		Messages:       repos.Messages,
		EventObserver: func(event tasklifecycle.TerminalEvent) {
			terminalEvents = append(terminalEvents, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Start(ctx, started.Task.ID); err != nil {
		t.Fatal(err)
	}
	boundary := len(boundaries)
	tx.fail = true
	if _, err := lifecycle.End(ctx, started.Task.ID, taskmodel.TaskCompleted, "", ""); !errors.Is(err, errRollback) {
		t.Fatal("Task 结算回滚错误丢失：", err)
	}
	if !reflect.DeepEqual(boundaries[boundary:], []string{"start:task_settle", "end:task_settle"}) || len(terminalEvents) != 0 {
		t.Fatalf("回滚缺少前后边界或发送了 UI 终态通知：%v", boundaries[boundary:])
	}
	if !errors.Is(attempts[len(attempts)-1].result.Err, errRollback) {
		t.Fatal("Task 结算回滚没有被 End 观察到")
	}
	boundary = len(boundaries)
	tx.fail = false
	for range 2 {
		if _, err := lifecycle.End(ctx, started.Task.ID, taskmodel.TaskCompleted, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(boundaries[boundary:], []string{
		"start:task_settle",
		"end:task_settle",
		"start:task_settle",
		"end:task_settle",
	}) || tx.operation != hooks.OperationTaskSettle || len(terminalEvents) != 1 {
		t.Fatalf("结算顺序、Context 传播或重试幂等性错误：%v", boundaries[boundary:])
	}
	start.ReleaseExecution(started.Task.AgentID, started.Task.ID)
	input.RequestID = "cancelled"
	cancelled, err := start.SendInput(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	control, err := agentmodel.NewAgentControlCommand("cancel", "agent", cancelled.Task.ID, agentmodel.AgentControlCancel, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InTx(ctx, func(txCtx context.Context) error { return repos.Controls.Save(txCtx, control) }); err != nil {
		t.Fatal(err)
	}
	// 已提交控制命令覆盖 Loop 的完成结果，Hook 必须使用事务最终仲裁的 Outcome。
	if _, err := lifecycle.End(ctx, cancelled.Task.ID, taskmodel.TaskCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	if attempts[len(attempts)-1].result.Outcome != taskmodel.TaskInterrupted || len(terminalEvents) != 2 || terminalEvents[1].Kind != tasklifecycle.TerminalRequestCanceled {
		t.Fatal("取消结果或 UI 终态通知丢失")
	}
	for _, attempt := range attempts {
		if attempt.ends != 1 {
			t.Fatalf("生命周期操作未恰好结束一次：%+v", attempt)
		}
	}
	if !errors.Is(attempts[0].result.Err, errRollback) {
		t.Fatal("Session 创建回滚没有被 End 观察到")
	}
}
