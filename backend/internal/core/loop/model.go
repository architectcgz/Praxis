package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
	"strings"
	"time"
)

// summarizeContext 在 Turn 之间生成归属于 Run 的摘要，不执行工具或发布回答文本。
func (r *Runner) summarizeContext(ctx context.Context, params RunParams, deps RunDependencies, input appcontext.ModelContext, limit int) (string, identity.ModelRequestID, identity.TurnID, error) {
	var turnID identity.TurnID
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
	response, err := r.Request(requestCtx, params, deps, request, sessionentry.ModelRequestSummary)
	if err != nil {
		return "", response.RequestID, turnID, err
	}
	if len(response.ToolCalls()) != 0 {
		return "", response.RequestID, turnID, errors.New("context summary returned a tool call")
	}
	return strings.TrimSpace(response.Text()), response.RequestID, turnID, nil
}
