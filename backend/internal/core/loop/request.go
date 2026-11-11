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

// summarizeContext 在独立 Turn 中生成摘要，不执行工具或发布回答文本。
func (r *Runner) summarizeContext(ctx context.Context, params RunParams, deps RunDependencies, input appcontext.ModelContext, limit int) (string, identity.ModelRequestID, identity.TurnID, error) {
	if err := deps.Recorder.EndTurn(ctx); err != nil {
		return "", "", "", err
	}
	turnID, err := deps.Recorder.BeginTurn(ctx)
	if err != nil {
		return "", "", "", err
	}
	requestID := identity.NewModelRequestID()
	request := providerapi.Request{
		ID:              requestID,
		TurnID:          turnID,
		RunID:           params.ID,
		PromptCacheKey:  params.SessionID.String(),
		Context:         input,
		Model:           params.Model,
		MaxOutputTokens: limit,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := r.Request(requestCtx, params, deps, request, hook.ModelRequestSummary)
	if err != nil {
		return "", requestID, turnID, err
	}
	if len(response.ToolCalls()) != 0 {
		return "", requestID, turnID, errors.New("context summary returned a tool call")
	}
	var output strings.Builder
	for _, event := range response.events {
		if event.Kind == providerapi.StreamTextDelta {
			output.WriteString(event.Text)
		}
	}
	return strings.TrimSpace(output.String()), requestID, turnID, nil
}
