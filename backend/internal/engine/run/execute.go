package run

import (
	"context"
	"errors"
	"praxis/internal/engine/hook"
	"praxis/internal/engine/loop"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/tool"
	"time"
)

const ErrInterrupted errorcode.Code = "run.interrupted"

// Prepare 提交 Run 和输入事实。返回值表示 RunPrepared 已写入历史。
func (r *Run) Prepare(ctx context.Context, prompt string) (bool, error) {
	if _, _, err := r.AppendEntry(ctx, entry.Entry{
		ID:        identity.NewEntryID(),
		CreatedAt: r.createdAt,
		Payload: entry.RunPrepared{
			AgentName:    r.definition.Name,
			DefinitionID: r.definition.ID,
			SourceTaskID: r.sourceTaskID,
		},
	}); err != nil {
		return false, err
	}
	inputID := identity.NewEntryID()
	authorKind := entry.AuthorAgent
	if r.Main() {
		inputID = identity.TaskInputEntryID(r.sourceTaskID)
		authorKind = entry.AuthorUser
	}
	stored, _, err := r.AppendEntry(ctx, entry.Entry{
		ID:         inputID,
		AuthorKind: authorKind,
		Payload: entry.Message{
			Role:    entry.RoleUser,
			Content: []entry.ContentBlock{entry.TextBlock{Text: prompt}},
		},
	})
	if err != nil {
		return true, err
	}
	r.input = stored
	r.deps.Emitter.Emit(event.Event{
		Source:  r.EventSource(""),
		Payload: event.EntriesAppended{Entries: []*entry.Entry{stored}},
	})
	if hookErr := r.deps.HookDispatcher.UserInputCommitted(context.WithoutCancel(ctx), hook.EntryCommitted{
		Identity: runIdentity(r, ""),
		Entry:    *stored,
	}); hookErr != nil {
		r.deps.Logger.Errorf("user input committed hook failed: %v", hookErr)
	}
	return true, nil
}

// Execute 即使在启动或 Loop 中途失败，也要等待子执行收尾并提交终态。
func (r *Run) Execute(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (tool.ToolCatalog, loop.ToolExecutor, error)) (err error) {
	ctx, scope := r.deps.HookDispatcher.Start(ctx, hook.Operation{
		Identity: runIdentity(r, ""),
		Kind:     hook.OperationAgent,
		Name:     r.definition.ID.String(),
	})
	defer func() {
		scope.End(hook.Result{
			Outcome: string(r.Outcome()),
			Err:     err,
		})
	}()
	if err = ctx.Err(); err == nil {
		startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		at := time.Now().UTC()
		_, _, saveErr := r.AppendEntry(startCtx, entry.Entry{
			ID:      identity.NewEntryID(),
			Payload: entry.RunStarted{At: at},
		})
		cancel()
		if saveErr != nil {
			err = storageFailure(saveErr)
		} else {
			r.deps.Emitter.Emit(event.Event{
				Source:  r.EventSource(""),
				Payload: event.RunStarted{StartedAt: at},
			})
			err = r.executeLoop(ctx, runner, prepareTools)
		}
	}
	r.finishChildren()
	if settleErr := r.settleExecution(ctx, err); settleErr != nil {
		return storageFailure(errors.Join(err, settleErr))
	}
	return err
}

func (r *Run) executeLoop(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (tool.ToolCatalog, loop.ToolExecutor, error)) error {
	if hookErr := r.deps.HookDispatcher.RunStarted(context.WithoutCancel(ctx), hook.RunEvent{Identity: runIdentity(r, "")}); hookErr != nil {
		r.deps.Logger.Errorf("run start hook failed: %v", hookErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	catalog, tools, err := prepareTools(ctx)
	if err != nil {
		return err
	}
	params, deps := r.modelRunParams(catalog)
	if _, err := runner.Run(ctx, params, deps, r.input, tools); err != nil {
		return err
	}
	if _, _, err := r.AppendEntry(context.WithoutCancel(ctx), entry.Entry{
		ID:      identity.NewEntryID(),
		Payload: entry.RunEndingStarted{},
	}); err != nil {
		return err
	}
	r.deps.Emitter.Emit(event.Event{
		Source:  r.EventSource(""),
		Payload: event.RunEnding{},
	})
	return nil
}
