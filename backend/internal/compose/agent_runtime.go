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
	"praxis/internal/hooks"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
	"praxis/internal/logging"
	"praxis/internal/loop"
	"praxis/internal/repository"
	runtimequeue "praxis/internal/service/runtime/queue"
	toolexecution "praxis/internal/tool_execution"
)

// newLoop 装配模型与持久化工具调用依赖，返回 runtime 调用的 loop.Run 执行函数。
func newLoop(
	modelBuilder model.ModelBuilder,
	toolConfig toolexecution.Config,
	builder loop.TaskContextBuilder,
	dispatcher *hooks.Dispatcher,
	observer agentruntime.AgentEventObserver,
	logf func(string, ...any),
) (agentruntime.LoopFunc, error) {
	toolCalls, err := toolexecution.NewExecutor(toolConfig)
	if err != nil {
		return nil, err
	}
	runner, err := loop.NewRunner(loop.Config{
		ModelBuilder:   modelBuilder,
		Tasks:          toolConfig.Tasks,
		Transactions:   toolConfig.Transactions,
		ContextBuilder: builder,
		HookDispatcher: dispatcher,
		Tools:          toolConfig.Catalog,
		ToolExecutor:   toolCalls,
		EventObserver:  observer,
		Logf:           logf,
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
	hooks            *hooks.Dispatcher
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
		HookDispatcher:   f.hooks,
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
