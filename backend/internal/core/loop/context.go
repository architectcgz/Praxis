package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
)

// BuildContext 只选择本轮所需的 Entry 引用；Hook 只读观察，容量评估在构建完成后执行。
func (r *Runner) BuildContext(ctx context.Context, params RunParams, entries []*sessionentry.ContextEntry) appcontext.ModelContext {
	built := appcontext.ModelContextFromEntries(params.SystemPrompt, entries)
	identity := runIdentity(params, "")
	identity.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.AfterContextBuild(context.WithoutCancel(ctx), hook.ContextBuilt{
		Identity: identity,
		Context:  built,
	}); hookErr != nil {
		r.Logf("after context build hook failed run=%s: %v", params.ID, hookErr)
	}
	return built
}
