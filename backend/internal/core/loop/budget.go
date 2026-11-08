package loop

import (
	"encoding/json"
	"fmt"
	appcontext "praxis/internal/core/context"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
)

// remindContextBudget 在待发送的请求上估算预算，提醒不回写 Loop 上下文或 Session Entry。
// 具体协议的编码差异仍由 Provider 在发送前进行硬校验。
func remindContextBudget(request providerapi.Request) (providerapi.Request, error) {
	tokens, err := request.Context.EstimatedTokens()
	if err != nil {
		return request, err
	}
	overhead, err := estimateRequestOverhead(request.Tools, request.OutputSchema)
	if err != nil {
		return request, err
	}
	inputTokens := tokens + overhead
	if appcontext.NearContextLimit(inputTokens, request.MaxOutputTokens, request.Model.ContextWindow) {
		request.Context.SystemPrompt += fmt.Sprintf("\n\n[运行时上下文预算提醒]\n当前估算输入为 %d tokens，输出预留为 %d tokens，模型窗口为 %d tokens，已达到窗口的 90%%。请优先整理已有结果并完成本次回答，避免启动新的长任务、扩大检索范围或读取大量内容；仍可执行完成当前任务所必需的工具调用。", inputTokens, request.MaxOutputTokens, request.Model.ContextWindow)
	}
	return request, nil
}

func estimateRequestOverhead(tools []tool.ToolDefinition, schema *providerapi.OutputSchema) (int, error) {
	metadata, err := json.Marshal(struct {
		Tools        []tool.ToolDefinition
		OutputSchema *providerapi.OutputSchema
	}{
		Tools:        tools,
		OutputSchema: schema,
	})
	if err != nil {
		return 0, err
	}
	return appcontext.EstimateTokens(string(metadata)), nil
}
