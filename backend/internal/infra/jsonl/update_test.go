package jsonl

import (
	"context"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"testing"
	"time"
)

func TestTaskUpdateUsesCoreRules(t *testing.T) {
	f := newStoreFixture(t)
	before := f.activeTask(t)
	ctx := context.Background()
	running := before
	requireOK(t, running.MarkRunning(time.Now()))
	running.Progress = taskmodel.Progress{
		Iterations:    2,
		ModelRequests: 3,
		NextSequence:  4,
		OutputBytes:   10,
		ToolCalls:     2,
	}
	requireOK(t, taskmodel.ValidateUpdate(before, running))
	requireOK(t, f.store.InTx(ctx, func(ctx context.Context) error { return f.repos.Tasks.Save(ctx, running) }))
	before = running
	for _, tc := range []struct {
		name   string
		mutate func(*taskmodel.Task)
	}{
		{"identity", func(v *taskmodel.Task) { v.SessionID = "other" }},
		{"budget_regression", func(v *taskmodel.Task) { v.Progress.Iterations-- }},
		{"output_regression", func(v *taskmodel.Task) { v.Progress.OutputBytes-- }},
		{"frozen_config", func(v *taskmodel.Task) { v.Config.SystemPrompt = "changed" }},
		{"backwards_state", func(v *taskmodel.Task) { v.Status = taskmodel.TaskStarting; v.StartedAt = time.Time{} }},
		{"start_time", func(v *taskmodel.Task) { v.StartedAt = v.StartedAt.Add(time.Second) }},
		{"iteration_overrun", func(v *taskmodel.Task) {
			v.Progress.Iterations = 31
			v.Progress.ModelRequests = 31
			v.Progress.NextSequence = 32
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := before
			tc.mutate(&after)
			if err := taskmodel.ValidateUpdate(before, after); err == nil {
				t.Fatal("core accepted illegal update")
			}
			if err := f.store.InTx(ctx, func(ctx context.Context) error { return f.repos.Tasks.Save(ctx, after) }); err == nil {
				t.Fatal("repository accepted illegal update")
			}
		})
	}
}

func TestToolUpdateUsesCoreRules(t *testing.T) {
	f := newStoreFixture(t)
	task := f.activeTask(t)
	f.toolCall(t, task)
	ctx := context.Background()
	before, err := f.repos.ToolInvocations.FindByTaskCall(ctx, task.ID, "call")
	requireOK(t, err)
	after := before
	after.BudgetReserved = true
	requireOK(t, toolmodel.ValidateUpdate(before, after))
	requireOK(t, f.store.InTx(ctx, func(ctx context.Context) error { return f.repos.ToolInvocations.Save(ctx, after) }))
	before = after
	for _, tc := range []struct {
		name   string
		mutate func(*toolmodel.ToolInvocation)
	}{
		{"budget_regression", func(v *toolmodel.ToolInvocation) { v.BudgetReserved = false }},
		{"identity", func(v *toolmodel.ToolInvocation) { v.ProviderToolCallID = "other" }},
		{"premature_normalization", func(v *toolmodel.ToolInvocation) { v.NormalizedArguments = []byte(`{}`) }},
		{"execution_without_approval", func(v *toolmodel.ToolInvocation) {
			v.Status = toolmodel.ToolInvocationRunning
			v.StartedAt = time.Now()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := before
			tc.mutate(&after)
			if err := toolmodel.ValidateUpdate(before, after); err == nil {
				t.Fatal("core accepted illegal update")
			}
			if err := f.store.InTx(ctx, func(ctx context.Context) error { return f.repos.ToolInvocations.Save(ctx, after) }); err == nil {
				t.Fatal("repository accepted illegal update")
			}
		})
	}
}
