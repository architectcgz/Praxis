package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
	"strings"
	"time"
)

// summarizeContext 在真实模型请求边界执行摘要，不执行工具、不发布思考或回答文本。
// 请求和输出由 Run Service 原生记录，不修改原始 Entry。
func (r *Runner) summarizeContext(
	ctx context.Context,
	params RunParams,
	deps RunDependencies,
	summaryInput appcontext.ModelContext,
	limit int,
) (string, error) {
	sequence, err := deps.Progress.NextRequestSequence(ctx)
	if err != nil {
		return "", err
	}
	turnID := identity.NewTurnID(params.ID, sequence)
	request := providerapi.Request{
		RunID:           params.ID,
		PromptCacheKey:  params.SessionID.String(),
		Context:         summaryInput,
		Model:           params.Model,
		MaxOutputTokens: limit,
		TurnID:          turnID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := r.Request(requestCtx, params, deps, request, hook.ModelRequestSummary, sequence)
	if err != nil {
		return "", err
	}
	if len(response.ToolCalls()) != 0 {
		return "", errors.New("context summary returned a tool call")
	}
	var text strings.Builder
	for _, event := range response.events {
		if event.Kind == providerapi.StreamTextDelta {
			text.WriteString(event.Text)
		}
	}
	return strings.TrimSpace(text.String()), nil
}
