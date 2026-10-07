package compose

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
	"praxis/internal/logging"
	"praxis/internal/loop"
	"praxis/internal/repository"
	runtimequeue "praxis/internal/service/runtime/queue"
	toolinvocation "praxis/internal/service/runtime/task/tool_invocation"
	taskturn "praxis/internal/service/runtime/task/turn"
	"praxis/internal/timing"
)

// newLoop 装配模型与持久化工具调用依赖，返回 runtime 调用的 loop.Run 执行函数。
func newLoop(
	modelBuilder model.ModelBuilder,
	toolConfig toolinvocation.Config,
	turnConfig taskturn.Config,
	recorder *timing.Recorder,
	observer agentruntime.AgentEventObserver,
	logf func(string, ...any),
) (agentruntime.LoopFunc, error) {
	toolCalls, err := toolinvocation.NewService(toolConfig)
	if err != nil {
		return nil, err
	}
	turns, err := taskturn.NewService(turnConfig)
	if err != nil {
		return nil, err
	}
	runner, err := loop.NewRunner(loop.Config{
		ModelBuilder:  timedModelBuilder{next: modelBuilder, recorder: recorder},
		Tools:         toolConfig.Catalog,
		ToolCalls:     timedToolCalls{next: toolCalls, recorder: recorder},
		Turns:         turns,
		EventObserver: observer,
		Logf:          logf,
	})
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, task taskmodel.Task, messageRecorder agentruntime.MessageRecorder) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
		return runner.Run(ctx, task, messageRecorder)
	}, nil
}

func newModelStream(
	format modelconfig.APIFormat,
	provider modelconfig.Provider,
	configured modelconfig.Model,
	apiKey string,
	client *http.Client,
) (model.ModelStream, error) {
	switch format {
	case modelconfig.APIFormatAnthropicMessages:
		return anthropicmessages.New(anthropicmessages.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: configured.ContextWindow,
		})
	case modelconfig.APIFormatOpenAIChatCompletions:
		return openaichat.New(openaichat.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: configured.ContextWindow,
		})
	case modelconfig.APIFormatOpenAIResponses:
		return openairesponses.New(openairesponses.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: configured.ContextWindow,
		})
	default:
		return nil, fmt.Errorf("unsupported model API format %q", format)
	}
}

type runtimeFactory struct {
	executions       *agentmodel.Executions
	runLoop          agentruntime.LoopFunc
	messageRecorders agentruntime.MessageRecorderResolver
	taskBuilder      agentruntime.TaskBuilder
	lifecycle        agentruntime.TaskLifecycle
	tasks            repository.TaskRepository
	logger           *logging.Logger
}

// New 为指定 Agent 创建独立执行槽，消息记录器在每次执行时解析。
func (f runtimeFactory) New(
	ctx context.Context,
	agentID contracts.AgentID,
) (agentruntime.ManagedRuntime, error) {
	queue, err := runtimequeue.NewTaskQueue(f.tasks, agentID)
	if err != nil {
		return nil, err
	}
	return agentruntime.NewRuntime(agentruntime.RuntimeConfig{
		AgentID:          agentID,
		Executions:       f.executions,
		MessageRecorders: f.messageRecorders,
		RunLoop:          f.runLoop,
		TaskBuilder:      f.taskBuilder,
		Lifecycle:        f.lifecycle,
		Queue:            queue,
		Logger:           f.logger,
		LifecycleTimeout: 30 * time.Second,
	})
}
