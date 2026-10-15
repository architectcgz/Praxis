package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/infra/jsonl"
	"praxis/internal/infra/toolconfig"
	"praxis/internal/request"
	runtimesvc "praxis/internal/service/runtime"
	"praxis/internal/tools"
)

type executionFixture struct {
	t         *testing.T
	root      string
	workspace string
	store     *jsonl.Store
	repos     jsonl.Repositories
	catalog   *toolconfig.Catalog
	executor  *Executor
	call      contracts.ToolCall
	metadata  contracts.ToolInvocationContext
}

func newExecutionFixture(t *testing.T, name contracts.ToolName, arguments any, action security.CommandAction) *executionFixture {
	t.Helper()
	f := &executionFixture{t: t, root: t.TempDir(), workspace: t.TempDir()}
	config := toolconfig.Config{Bash: security.CommandPolicy{DefaultAction: action}}
	f.catalog = toolconfig.NewCatalog(config, tools.NewToolRegistry())
	f.open()
	t.Cleanup(func() {
		if err := f.store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	at := time.Now().UTC()
	project, err := projectmodel.NewProject("project", "项目", f.workspace, "workspace", at)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspacemodel.NewWorkspace("workspace", project.ID, workspacemodel.WorkspaceProjectRoot, f.workspace, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if err := f.repos.Projects.Save(ctx, project); err != nil {
			return err
		}
		return f.repos.Workspaces.Save(ctx, workspace)
	}); err != nil {
		t.Fatal(err)
	}
	session, err := sessionmodel.NewSession("session", project.ID, workspace.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := agentmodel.NewAgent("agent", session.ID, agentmodel.DefinitionPrimary, contracts.ProfilePrimary, at)
	if err != nil {
		t.Fatal(err)
	}
	task, err := taskmodel.NewTask("task", session.ID, agent.ID, "request", taskmodel.InputSnapshot{
		AgentDefinitionID: agent.DefinitionID, AgentDefinitionRevision: "revision", CurrentInputMessageID: "input",
		WorkspacePath: f.workspace,
		Model: model.ModelSnapshot{
			ProviderID:      "provider",
			ModelID:         "model",
			APIFormat:       "openai_chat",
			ContextWindow:   8192,
			MaxOutputTokens: 1024,
			BaseURL:         "https://example.com",
		},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(task.ID, at); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	f.call = contracts.ToolCall{ID: "call", Name: name, Arguments: encoded}
	f.metadata = contracts.ToolInvocationContext{
		TaskID:        task.ID,
		SessionID:     session.ID,
		AgentID:       agent.ID,
		TurnID:        contracts.NewTurnID(task.ID, 1),
		WorkspacePath: f.workspace,
	}
	invocation, err := toolmodel.NewToolInvocation("invocation", task.ID, f.metadata.TurnID, session.ID, agent.ID, f.call.ID, name, encoded, toolmodel.ArgumentsDigest(name, encoded), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if err := f.repos.Sessions.Save(ctx, session); err != nil {
			return err
		}
		if err := f.repos.Agents.Save(ctx, agent); err != nil {
			return err
		}
		if err := f.repos.Tasks.Save(ctx, task); err != nil {
			return err
		}
		return f.repos.ToolInvocations.Save(ctx, invocation)
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *executionFixture) open() {
	f.t.Helper()
	var err error
	f.store, err = jsonl.Open(f.t.Context(), f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	f.repos = f.store.Repositories()
	f.executor, err = NewExecutor(Config{
		CommandPolicy: f.catalog,
		Transactions:  f.store,
		Tasks:         f.repos.Tasks,
		Invocations:   f.repos.ToolInvocations,
		Catalog:       f.catalog,
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *executionFixture) invocation() toolmodel.ToolInvocation {
	f.t.Helper()
	invocation, err := f.repos.ToolInvocations.Get(f.t.Context(), "invocation")
	if err != nil {
		f.t.Fatal(err)
	}
	return invocation
}

func (f *executionFixture) admit() toolmodel.ToolInvocation {
	f.t.Helper()
	tool, _ := f.catalog.Get(f.call.Name)
	normalized, err := tool.Normalize(f.call)
	if err != nil {
		f.t.Fatal(err)
	}
	normalized, err = resolveNormalizedPath(normalized, f.workspace)
	if err != nil {
		f.t.Fatal(err)
	}
	invocation, err := f.executor.admit(f.t.Context(), f.invocation(), normalized)
	if err != nil {
		f.t.Fatal(err)
	}
	return invocation
}

func (f *executionFixture) reload(action security.CommandAction) {
	f.t.Helper()
	candidate := toolconfig.NewCatalog(toolconfig.Config{Bash: security.CommandPolicy{DefaultAction: action}}, tools.NewToolRegistry())
	if err := f.store.InTx(f.t.Context(), func(context.Context) error { f.catalog.ReplaceFrom(candidate); return nil }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *executionFixture) approvals() runtimesvc.ToolApprovals {
	return runtimesvc.ToolApprovals{
		CommandPolicy: f.catalog,
		Transactions:  f.store,
		Tasks:         f.repos.Tasks,
		Invocations:   f.repos.ToolInvocations,
	}
}

func (f *executionFixture) decision() request.ToolDecision {
	invocation := f.invocation()
	return request.ToolDecision{
		SessionID:       f.metadata.SessionID,
		AgentID:         f.metadata.AgentID,
		TaskID:          f.metadata.TaskID,
		CallID:          f.call.ID,
		ArgumentsDigest: toolmodel.ArgumentsDigest(invocation.Name, invocation.NormalizedArguments),
	}
}

func TestCommandAdmissionAndExecution(t *testing.T) {
	for _, test := range []struct {
		name    string
		action  security.CommandAction
		status  toolmodel.ToolInvocationStatus
		pending bool
	}{
		{name: "自动执行", action: security.CommandAllow, status: toolmodel.ToolInvocationSucceeded},
		{
			name:    "默认审核",
			action:  security.CommandAsk,
			status:  toolmodel.ToolInvocationAwaitingApproval,
			pending: true,
		},
		{name: "直接拒绝", action: security.CommandDeny, status: toolmodel.ToolInvocationDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "printf verified"}, test.action)
			result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
			if err != nil || result.ApprovalPending != test.pending || f.invocation().Status != test.status {
				t.Fatalf("执行结果错误：%+v %v %+v", result, err, f.invocation())
			}
			if test.action == security.CommandAllow && !json.Valid([]byte(result.Result.Payload)) {
				t.Fatal("Bash 未返回执行结果")
			}
		})
	}
}

func TestPolicyChangesBeforeStart(t *testing.T) {
	for _, test := range []struct {
		name          string
		initial, next security.CommandAction
		user          bool
		status        toolmodel.ToolInvocationStatus
	}{
		{
			name:    "人工批准后禁止",
			initial: security.CommandAsk,
			next:    security.CommandDeny,
			user:    true,
			status:  toolmodel.ToolInvocationInterrupted,
		},
		{
			name:    "自动批准后要求审核",
			initial: security.CommandAllow,
			next:    security.CommandAsk,
			status:  toolmodel.ToolInvocationInterrupted,
		},
		{
			name:    "待审核恢复仍等待",
			initial: security.CommandAsk,
			next:    security.CommandAllow,
			status:  toolmodel.ToolInvocationAwaitingApproval,
		},
		{
			name:    "待审核后禁止",
			initial: security.CommandAsk,
			next:    security.CommandDeny,
			status:  toolmodel.ToolInvocationDenied,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "touch should-not-exist"}, test.initial)
			f.admit()
			if test.user {
				if _, err := f.approvals().Decide(t.Context(), f.decision(), true); err != nil {
					t.Fatal(err)
				}
			}
			f.reload(test.next)
			invocation, execute, err := f.executor.start(t.Context(), "invocation")
			if err != nil || execute || invocation.Status != test.status {
				t.Fatalf("配置变更后错误执行：%+v %v %v", invocation, execute, err)
			}
			if _, err := os.Stat(filepath.Join(f.workspace, "should-not-exist")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("被禁止的命令产生了副作用")
			}
		})
	}
}

func TestApprovalBindsCanonicalArgumentsAndSurvivesRestart(t *testing.T) {
	f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "printf approved", "timeout": 7}, security.CommandAsk)
	result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
	if err != nil || !result.ApprovalPending {
		t.Fatal(result, err)
	}
	approved := f.decision()
	wrong := approved
	wrong.ArgumentsDigest = "stale"
	if _, err := f.approvals().Decide(t.Context(), wrong, true); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatalf("错误参数审批被接受：%v", err)
	}
	for _, field := range []string{"command", "path", "timeout"} {
		if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
			invocation, err := f.repos.ToolInvocations.Get(ctx, "invocation")
			if err != nil {
				return err
			}
			var arguments map[string]any
			if err := json.Unmarshal(invocation.NormalizedArguments, &arguments); err != nil {
				return err
			}
			arguments[field] = "changed"
			invocation.NormalizedArguments, _ = json.Marshal(arguments)
			return f.repos.ToolInvocations.Save(ctx, invocation)
		}); !errors.Is(err, contracts.ErrRequestConflict) {
			t.Fatalf("冻结参数 %s 可被篡改：%v", field, err)
		}
	}
	if err := f.store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.open()
	result, err = f.executor.Execute(t.Context(), f.call, f.metadata)
	if err != nil || !result.ApprovalPending {
		t.Fatal("重启丢失审批等待", result, err)
	}
	if _, err := f.approvals().Decide(t.Context(), approved, true); err != nil {
		t.Fatal(err)
	}
	result, err = f.executor.Execute(t.Context(), f.call, f.metadata)
	if err != nil || result.ApprovalPending || f.invocation().Status != toolmodel.ToolInvocationSucceeded {
		t.Fatal(result, err)
	}
	invocation := f.invocation()
	if invocation.Approval.ArgumentsDigest != approved.ArgumentsDigest {
		t.Fatal("审批未绑定完整参数")
	}
	task, err := f.repos.Tasks.Get(t.Context(), f.metadata.TaskID)
	if err != nil || task.Progress.ToolCalls != 1 {
		t.Fatal("审批与恢复重复扣除工具额度", task.Progress, err)
	}
}

func TestUserCannotApproveDeniedCommand(t *testing.T) {
	f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "printf prohibited"}, security.CommandAsk)
	f.admit()
	f.reload(security.CommandDeny)
	if _, err := f.approvals().Decide(t.Context(), f.decision(), true); err != nil {
		t.Fatal(err)
	}
	if f.invocation().Status != toolmodel.ToolInvocationDenied {
		t.Fatal("deny 被用户批准绕过")
	}
}

func TestFileToolsUseWorkspaceWithoutApproval(t *testing.T) {
	t.Run("read_file", func(t *testing.T) {
		f := newExecutionFixture(t, contracts.ToolReadFile, map[string]any{"path": "hello.txt"}, security.CommandDeny)
		if err := os.WriteFile(filepath.Join(f.workspace, "hello.txt"), []byte("hello"), 0600); err != nil {
			t.Fatal(err)
		}
		result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
		if err != nil || result.ApprovalPending || f.invocation().Status != toolmodel.ToolInvocationSucceeded {
			t.Fatal(result, err)
		}
	})
	t.Run("apply_patch", func(t *testing.T) {
		f := newExecutionFixture(t, contracts.ToolApplyPatch, map[string]any{"patch": "*** Begin Patch\n*** Add File: hello.txt\n+hello\n*** End Patch"}, security.CommandDeny)
		result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
		if err != nil || result.ApprovalPending || f.invocation().Status != toolmodel.ToolInvocationSucceeded {
			t.Fatal(result, err)
		}
		if content, err := os.ReadFile(filepath.Join(f.workspace, "hello.txt")); err != nil || string(content) != "hello\n" {
			t.Fatal(string(content), err)
		}
	})
	t.Run("拒绝读取工作区以外文件", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
			t.Fatal(err)
		}
		f := newExecutionFixture(t, contracts.ToolReadFile, map[string]any{"path": outside}, security.CommandAllow)
		result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
		if err != nil || f.invocation().Status != toolmodel.ToolInvocationFailed || result.Result.ErrorClass == "" {
			t.Fatal(result, err)
		}
	})
}

func TestRestartedUnknownExecutionIsNotRetried(t *testing.T) {
	f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "touch duplicate-marker"}, security.CommandAllow)
	agent, err := f.repos.Agents.Get(t.Context(), f.metadata.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.repos.Messages.Append(t.Context(), agent, sessionmodel.MessageData{
		ID: "assistant", RequestID: "request", TaskID: f.metadata.TaskID.String(), TurnID: f.metadata.TurnID.String(),
		Role: sessionmodel.RoleAssistant, AuthorKind: sessionmodel.AuthorAgent, AuthorID: agent.ID.String(), CreatedAt: time.Now().UTC(),
		Blocks: []sessionmodel.Block{{
			Kind:   sessionmodel.BlockToolCall,
			CallID: f.call.ID,
			Name:   string(f.call.Name),
			Input:  f.call.Arguments,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.admit()
	if _, execute, err := f.executor.start(t.Context(), "invocation"); err != nil || !execute {
		t.Fatal(execute, err)
	}
	if err := f.store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.open()
	if _, err := f.repos.Messages.LoadMessages(t.Context(), f.metadata.SessionID, f.metadata.AgentID, 0); err != nil {
		t.Fatal(err)
	}
	if f.invocation().Status != toolmodel.ToolInvocationUnknown {
		t.Fatal("重启未修复结果未知状态")
	}
	result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
	if err != nil || result.Result.ErrorClass != string(toolmodel.ToolFailureResultUnknown) {
		t.Fatal(result, err)
	}
	retry, err := toolmodel.NewToolInvocation("retry", f.metadata.TaskID, f.metadata.TurnID, f.metadata.SessionID, f.metadata.AgentID, "retry-call", f.call.Name, f.call.Arguments, toolmodel.ArgumentsDigest(f.call.Name, f.call.Arguments), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error { return f.repos.ToolInvocations.Save(ctx, retry) }); err != nil {
		t.Fatal(err)
	}
	call := f.call.Snapshot()
	call.ID = retry.ProviderToolCallID
	result, err = f.executor.Execute(t.Context(), call, f.metadata)
	if err != nil || result.Result.ErrorClass != string(toolmodel.ToolFailureInterrupted) {
		t.Fatal("相同未知调用被重试", result, err)
	}
	if _, err := os.Stat(filepath.Join(f.workspace, "duplicate-marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("未知调用被执行")
	}
}

func TestDisabledToolCannotStartAfterApproval(t *testing.T) {
	f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "printf disabled"}, security.CommandAllow)
	f.admit()
	candidate := toolconfig.NewCatalog(toolconfig.Config{
		Tools: map[contracts.ToolName]toolconfig.ToolConfig{contracts.ToolBash: {Enabled: false}},
		Bash:  security.CommandPolicy{DefaultAction: security.CommandAllow},
	}, tools.NewToolRegistry())
	if err := f.store.InTx(t.Context(), func(context.Context) error { f.catalog.ReplaceFrom(candidate); return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
	if err != nil || result.Result.ErrorClass != string(toolmodel.ToolFailureInterrupted) || f.invocation().Status != toolmodel.ToolInvocationInterrupted {
		t.Fatal(result, err)
	}
}

func TestExactAllowDoesNotAdmitShellComposition(t *testing.T) {
	for _, command := range []string{"printf verified", "printf verified; touch unwanted", "printf verified && touch unwanted", "printf verified | cat"} {
		t.Run(command, func(t *testing.T) {
			f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": command}, security.CommandAsk)
			config := toolconfig.Config{Bash: security.CommandPolicy{
				DefaultAction: security.CommandAsk,
				Rules:         []security.CommandRule{{ID: "allow-printf", Action: security.CommandAllow, Match: security.CommandMatch{Type: "exact", Value: "printf verified"}}},
			}}
			if err := f.store.InTx(t.Context(), func(context.Context) error {
				f.catalog.ReplaceFrom(toolconfig.NewCatalog(config, tools.NewToolRegistry()))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			result, err := f.executor.Execute(t.Context(), f.call, f.metadata)
			if err != nil {
				t.Fatal(err)
			}
			if command == "printf verified" {
				if result.ApprovalPending || f.invocation().CommandDecision.RuleID != "allow-printf" {
					t.Fatal(result)
				}
			} else if !result.ApprovalPending {
				t.Fatalf("额外 Shell 操作被放行：%q", command)
			}
			if _, err := os.Stat(filepath.Join(f.workspace, "unwanted")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("未审核命令被执行")
			}
		})
	}
}

func TestUserDecisionIsIdempotentWithoutApproverIdentity(t *testing.T) {
	for _, approve := range []bool{true, false} {
		name := "批准"
		if !approve {
			name = "拒绝"
		}
		t.Run(name, func(t *testing.T) {
			f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "printf decided"}, security.CommandAsk)
			f.admit()
			params := f.decision()
			if _, err := f.approvals().Decide(t.Context(), params, approve); err != nil {
				t.Fatal(err)
			}
			if _, err := f.executor.Execute(t.Context(), f.call, f.metadata); err != nil {
				t.Fatal(err)
			}
			before := f.invocation()
			if err := f.store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.open()
			if _, err := f.approvals().Decide(t.Context(), params, approve); err != nil {
				t.Fatal("重启后重复审批不应报错", err)
			}
			if !reflect.DeepEqual(before, f.invocation()) {
				t.Fatal("重复审批改写了已保存的决定")
			}
			if _, err := f.approvals().Decide(t.Context(), params, !approve); !errors.Is(err, contracts.ErrRequestConflict) {
				t.Fatalf("相反的审批决定未被拒绝：%v", err)
			}
			params.ArgumentsDigest = "wrong-digest"
			if _, err := f.approvals().Decide(t.Context(), params, approve); !errors.Is(err, contracts.ErrRequestConflict) {
				t.Fatalf("重复审批绕过了参数绑定：%v", err)
			}
		})
	}
}

func TestPolicyDecisionDoesNotCountAsUserDecision(t *testing.T) {
	for _, action := range []security.CommandAction{security.CommandAllow, security.CommandDeny} {
		t.Run(string(action), func(t *testing.T) {
			f := newExecutionFixture(t, contracts.ToolBash, map[string]any{"command": "printf policy"}, action)
			f.admit()
			for _, approve := range []bool{true, false} {
				if _, err := f.approvals().Decide(t.Context(), f.decision(), approve); !errors.Is(err, contracts.ErrRequestConflict) {
					t.Fatalf("策略决定被误当成人工重复审批：%v", err)
				}
			}
		})
	}
}
