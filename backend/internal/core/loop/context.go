package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
)

// buildContext 只变换本轮请求副本，不把 Hook 输出回写 Run Context 或 Session Entry。
func (r *Runner) buildContext(ctx context.Context, input Input, base appcontext.ModelContext) (appcontext.ModelContext, error) {
	value := base.Clone()
	identity := executionIdentity(input, "")
	identity.OperationID = hook.NewOperationID()
	limits := appcontext.Limits{
		ContextWindow:   input.Model.ContextWindow,
		MaxOutputTokens: input.Model.MaxOutputTokens,
	}
	content, err := r.HookDispatcher.BeforeContextBuild(ctx, hook.ContextBuild{
		Identity: identity,
		Content: appcontext.Content{
			SystemPrompt: value.SystemPrompt,
			Entries:      value.Entries,
		},
		Limits: limits,
	})
	if err != nil {
		return appcontext.ModelContext{}, NewFailure(FailureInternal, err)
	}
	built, err := (appcontext.ContextBuilder{}).Build(content, limits)
	if hookErr := r.HookDispatcher.AfterContextBuild(context.WithoutCancel(ctx), hook.ContextBuilt{
		Identity: identity,
		Context:  built,
		Err:      err,
	}); hookErr != nil {
		r.Logf("after context build hook failed execution=%s: %v", input.ID, hookErr)
	}
	return built, err
}
