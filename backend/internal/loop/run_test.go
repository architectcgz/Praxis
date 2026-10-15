package loop

import (
	"context"
	"errors"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/tools"
	toolcontracts "praxis/internal/tools/contracts"
	"testing"
)

type budgetProgress struct {
	task taskmodel.Task
	fail bool
}

func (p *budgetProgress) LoadTask(context.Context, contracts.TaskID) (taskmodel.Task, error) {
	return p.task, nil
}
func (p *budgetProgress) RecordModelRequest(_ context.Context, _ contracts.TaskID, sequence uint64) error {
	if p.fail {
		return errors.New("storage failed")
	}
	p.task.Progress.NextSequence = sequence + 1
	p.task.Progress.ModelRequests++
	return nil
}
func (p *budgetProgress) RecordModelOutput(_ context.Context, _ contracts.TaskID, bytes int) error {
	p.task.Progress.OutputBytes += int64(bytes)
	return nil
}

type budgetModel struct{ requests int }

func (m *budgetModel) BuildModel(config model.Config) (model.Model, error) {
	return model.Model{Stream: m, MaxOutputTokens: config.MaxOutputTokens}, nil
}
func (m *budgetModel) Stream(context.Context, model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	m.requests++
	events := make(chan model.ModelStreamEvent, 2)
	events <- model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "output"}
	events <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted}
	close(events)
	return events, nil
}

type noTools struct{}

func (noTools) Execute(context.Context, contracts.ToolCall, contracts.ToolInvocationContext) (toolcontracts.ToolExecutionResult, error) {
	panic("unexpected tool execution")
}

type budgetRecorder struct{}

func (budgetRecorder) Append(_ context.Context, message sessionmodel.MessageData) (sessionmodel.MessageData, error) {
	return message, nil
}
func (budgetRecorder) AppendAssistant(context.Context, sessionmodel.MessageData, contracts.ToolInvocationContext) error {
	return nil
}

func TestLoopUsesPersistedBudgetsAndRecordsSummaryConsumption(t *testing.T) {
	cases := []struct {
		name     string
		limits   contracts.ResourceLimits
		spent    int64
		fail     bool
		requests int
		want     contracts.TaskFailureCode
	}{
		{name: "input", limits: contracts.ResourceLimits{MaxInputBytes: 1}, want: contracts.TaskFailureResourceLimit},
		{
			name:   "restored output",
			limits: contracts.ResourceLimits{MaxOutputBytes: 5},
			spent:  6,
			want:   contracts.TaskFailureResourceLimit,
		},
		{
			name:     "cumulative output",
			limits:   contracts.ResourceLimits{MaxOutputBytes: 8},
			spent:    4,
			requests: 1,
			want:     contracts.TaskFailureResourceLimit,
		},
		{name: "progress failure", fail: true, want: contracts.TaskFailureStorage},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			progress := &budgetProgress{task: taskmodel.Task{
				ID:        "task",
				AgentID:   "agent",
				SessionID: "session",
				Config:    taskmodel.Config{ResourceLimits: test.limits, Model: model.Config{ContextWindow: 8192, MaxOutputTokens: 1024}},
				Progress:  taskmodel.Progress{NextSequence: 7, ModelRequests: 6, OutputBytes: test.spent},
			}, fail: test.fail}
			provider := &budgetModel{}
			runner, err := NewRunner(Config{
				Progress:      progress,
				ModelBuilder:  provider,
				Tools:         tools.NewToolRegistry(),
				ToolExecutor:  noTools{},
				EventObserver: func(Event) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			content := appcontext.Content{Entries: []appcontext.ContextEntry{{
				Kind:      appcontext.ContextEntryUserInput,
				Role:      appcontext.ContextRoleUser,
				Protected: true,
				Content:   []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "input"}},
			}}}
			_, code, err := runner.Run(t.Context(), progress.task, content, nil, budgetRecorder{})
			if err == nil || code != test.want || provider.requests != test.requests {
				t.Fatalf("预算或存储失败未阻止请求: code=%s err=%v requests=%d", code, err, provider.requests)
			}
			if test.requests > 0 && (progress.task.Progress.OutputBytes != test.spent+int64(len("output")) || progress.task.Progress.NextSequence != 8) {
				t.Fatalf("消耗丢失: %+v", progress.task.Progress)
			}
		})
	}
	// 摘要和正式模型请求沿用同一个预算和单调递增序号。
	progress := &budgetProgress{task: taskmodel.Task{
		ID:        "task",
		AgentID:   "agent",
		SessionID: "session",
		Config:    taskmodel.Config{Model: model.Config{ContextWindow: 8192, MaxOutputTokens: 1024}},
		Progress:  taskmodel.Progress{NextSequence: 4, ModelRequests: 3, OutputBytes: 2},
	}}
	provider := &budgetModel{}
	runner, err := NewRunner(Config{
		Progress:            progress,
		ModelBuilder:        provider,
		Tools:               tools.NewToolRegistry(),
		ToolExecutor:        noTools{},
		EventObserver:       func(Event) {},
		CompressionStrategy: summaryOnce{},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := appcontext.Content{Entries: []appcontext.ContextEntry{{
		Kind:      appcontext.ContextEntryUserInput,
		Role:      appcontext.ContextRoleUser,
		Protected: true,
		Content:   []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "input"}},
	}}}
	outcome, _, err := runner.Run(t.Context(), progress.task, content, nil, budgetRecorder{})
	if err != nil || outcome != taskmodel.TaskCompleted || provider.requests != 2 || progress.task.Progress.NextSequence != 6 || progress.task.Progress.ModelRequests != 5 || progress.task.Progress.OutputBytes != 2+2*int64(len("output")) {
		t.Fatalf("摘要消耗或序号错误: %+v, %v", progress.task.Progress, err)
	}
}

type summaryOnce struct{}

func (summaryOnce) Compress(ctx context.Context, value appcontext.ModelContext, _ contextcompress.Options, summarize contextcompress.SummarizeFunc) (appcontext.ModelContext, bool, error) {
	_, err := summarize(ctx, value, 128)
	return value, false, err
}
