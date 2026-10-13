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
	"praxis/internal/hooks"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
	"praxis/internal/logging"
	"praxis/internal/loop"
	"praxis/internal/repository"
	runtimequeue "praxis/internal/service/runtime/queue"
)

// newLoop 校验已装配的模型、上下文、进度和工具 Port，返回 Runtime 的执行入口。
// 缺少依赖时返回错误，不创建额外的任务服务或工具执行器。
func newLoop(config loop.Config) (agentruntime.LoopFunc, error) {
	runner, err := loop.NewRunner(config)
	if err != nil {
		return nil, err
	}
	return runner.Run, nil
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
	taskPreparer     agentruntime.TaskPreparer
	transitions      agentruntime.TaskTransitions
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
		AgentID:           agentID,
		HookDispatcher:    f.hooks,
		Executions:        f.executions,
		MessageRecorders:  f.messageRecorders,
		RunLoop:           f.runLoop,
		TaskPreparer:      f.taskPreparer,
		Transitions:       f.transitions,
		Queue:             queue,
		Logger:            f.logger,
		TransitionTimeout: 30 * time.Second,
	})
}
