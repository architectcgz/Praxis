// Package agentruntime 组装 Agent execution 的模型循环与工具调用流程。
package agentruntime

import (
	"praxis/internal/loop"
	agent "praxis/internal/runtime/agent"
	toolinvocation "praxis/internal/service/execution/tool_invocation"
)

type RunnerConfig struct {
	ModelBuilder    loop.ExecutionModelBuilder
	ToolInvocations toolinvocation.Config
	EventObserver   agent.AgentEventObserver
	Logf            func(string, ...any)
}

// NewRunner 组装默认的 Agent execution runner；依赖缺失时返回错误。
func NewRunner(config RunnerConfig) (agent.ExecutionRunner, error) {
	toolCalls, err := toolinvocation.NewService(config.ToolInvocations)
	if err != nil {
		return nil, err
	}
	return loop.NewExecutionEngine(loop.ExecutionEngineConfig{
		ModelBuilder:  config.ModelBuilder,
		Tools:         config.ToolInvocations.Catalog,
		ToolCalls:     toolCalls,
		EventObserver: config.EventObserver,
		Logf:          config.Logf,
	})
}
