package loop

import (
	"encoding/json"
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"slices"
)

func requestShape(request providerapi.Request) (string, int, error) {
	data, err := json.Marshal(struct {
		Model  any
		Tools  []tool.ToolDefinition
		Schema *providerapi.OutputSchema
	}{Model: request.Model, Tools: request.Tools, Schema: request.OutputSchema})
	if err != nil {
		return "", 0, err
	}
	overhead, err := estimateRequestOverhead(request.Tools, request.OutputSchema)
	return string(data), overhead, err
}
func requestTokens(request providerapi.Request, budget *appcontext.Budget) (int, string, error) {
	key, overhead, err := requestShape(request)
	if err != nil {
		return 0, "", err
	}
	if budget != nil {
		tokens, err := budget.Estimate(request.Context, key, overhead)
		return tokens, key, err
	}
	tokens, err := request.Context.EstimatedTokens()
	return tokens + overhead + 128, key, err
}
func remindContextBudget(request providerapi.Request, budget *appcontext.Budget) (providerapi.Request, error) {
	tokens, _, err := requestTokens(request, budget)
	if err != nil {
		return request, err
	}
	if appcontext.NearContextLimit(tokens, request.MaxOutputTokens, request.Model.ContextWindow) {
		hint := &sessionentry.ContextEntry{Role: role.User, Content: []sessionentry.ContentBlock{sessionentry.TextBlock{Text: fmt.Sprintf("[本次请求的运行提示] 估算输入 %d tokens，输出预留 %d tokens，窗口 %d tokens。请优先完成当前任务，并按需分段读取工具输出。", tokens, request.MaxOutputTokens, request.Model.ContextWindow)}}}
		request.Context.Entries = append(slices.Clone(request.Context.Entries), hint)
		tokens, _, err = requestTokens(request, budget)
		if err != nil {
			return request, err
		}
	}
	return request, appcontext.CheckCapacity(tokens, request.MaxOutputTokens, request.Model.ContextWindow)
}
func estimateRequestOverhead(tools []tool.ToolDefinition, schema *providerapi.OutputSchema) (int, error) {
	data, err := json.Marshal(struct {
		Tools        []tool.ToolDefinition
		OutputSchema *providerapi.OutputSchema
	}{Tools: tools, OutputSchema: schema})
	if err != nil {
		return 0, err
	}
	return appcontext.EstimateTokens(string(data)), nil
}
