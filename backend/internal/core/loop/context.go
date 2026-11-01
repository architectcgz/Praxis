package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
)

// BuildContext 只变换本轮请求副本，不把 Hook 输出回写 Run Context 或 Session Entry。
func (r *Runner) BuildContext(ctx context.Context, execution ExecutionParams, base appcontext.ModelContext) (appcontext.ModelContext, error) {
	value := base.Clone()
	identity := executionIdentity(execution, "")
	identity.OperationID = hook.NewOperationID()
	limits := appcontext.Limits{
		ContextWindow:   execution.Model.ContextWindow,
		MaxOutputTokens: execution.Model.MaxOutputTokens,
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
		r.Logf("after context build hook failed execution=%s: %v", execution.ID, hookErr)
	}
	return built, err
}
