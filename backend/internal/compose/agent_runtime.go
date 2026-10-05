package compose

import (
	"context"
	"fmt"
	"net/http"
	"time"

	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	modelregistry "praxis/internal/infra/model_registry"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
	"praxis/internal/loop"
	toolinvocation "praxis/internal/service/turn/tool_invocation"
	"praxis/internal/timing"
)

// newTurnRunner 将持久化工具调用服务注入模型循环，运行时不依赖具体循环实现。
func newTurnRunner(
	modelBuilder loop.TurnModelBuilder,
	toolConfig toolinvocation.Config,
	recorder *timing.Recorder,
	observer agentruntime.AgentEventObserver,
	logf func(string, ...any),
) (agentruntime.TurnRunner, error) {
	toolCalls, err := toolinvocation.NewService(toolConfig)
	if err != nil {
		return nil, err
	}
	return loop.NewTurnEngine(loop.TurnEngineConfig{
		ModelBuilder:  timedModelBuilder{next: modelBuilder, recorder: recorder},
		Tools:         toolConfig.Catalog,
		ToolCalls:     timedToolCalls{next: toolCalls, recorder: recorder},
		EventObserver: observer,
		Logf:          logf,
	})
}

func newModelStream(
	format modelregistry.ModelAPIFormat,
	provider modelregistry.ProviderConfig,
	modelConfig modelregistry.ModelConfig,
	apiKey string,
	client *http.Client,
) (agentruntime.ModelStream, error) {
	switch format {
	case modelregistry.APIFormatAnthropicMessages:
		return anthropicmessages.New(anthropicmessages.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: modelConfig.ContextWindow,
		})
	case modelregistry.APIFormatOpenAIChatCompletions:
		return openaichat.New(openaichat.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: modelConfig.ContextWindow,
		})
	case modelregistry.APIFormatOpenAIResponses:
		return openairesponses.New(openairesponses.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: modelConfig.ContextWindow,
		})
	default:
		return nil, fmt.Errorf("unsupported model API format %q", format)
	}
}

type runtimeFactory struct {
	runner        agentruntime.TurnRunner
	messages      agentruntime.MessageStoreResolver
	logger        agentruntime.TurnLogger
	eventLogger   agentruntime.TurnEventLogger
	eventObserver agentruntime.AgentEventObserver
}

// New 为指定 Agent 创建独立执行槽，消息存储在每次执行时解析。
func (f runtimeFactory) New(
	ctx context.Context,
	agentID contracts.AgentID,
) (agentruntime.ManagedRuntime, error) {
	return agentruntime.NewRuntime(agentruntime.RuntimeConfig{
		AgentID:           agentID,
		Messages:          f.messages,
		Runner:            f.runner,
		Logger:            f.logger,
		EventLogger:       f.eventLogger,
		EventObserver:     f.eventObserver,
		SettlementTimeout: 30 * time.Second,
	})
}
