package run

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
	"time"
)

const ErrInterrupted errorcode.Code = "run.interrupted"

// Prepare 提交 Run 和输入事实。返回值表示 RunPrepared 已写入历史。
func (r *Run) Prepare(ctx context.Context, prompt string) (bool, error) {
	if _, _, err := r.AppendEntry(ctx, sessionentry.Entry{
		ID:        "prepared:" + r.id.String(),
		CreatedAt: r.createdAt,
		Payload: sessionentry.RunPrepared{
			AgentName:    r.definition.Name,
			DefinitionID: r.definition.ID,
			SourceTaskID: r.sourceTaskID,
		},
	}); err != nil {
		return false, err
	}
	inputID := "input:" + r.id.String()
	authorKind := sessionentry.AuthorAgent
	if r.Main() {
		inputID = sessionentry.TaskInputID(r.sourceTaskID)
		authorKind = sessionentry.AuthorUser
	}
	stored, _, err := r.AppendEntry(ctx, sessionentry.Entry{
		ID:         inputID,
		AuthorKind: authorKind,
		Payload: sessionentry.Message{
			Role:    role.User,
			Content: []sessionentry.ContentBlock{sessionentry.TextBlock{Text: prompt}},
		},
	})
	if err != nil {
		return true, err
	}
	r.input = stored
	r.deps.Emitter.Emit(coreevent.Event{
		Source:  r.EventSource(""),
		Payload: coreevent.EntriesAppended{Entries: []sessionentry.Entry{*stored}},
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
func (r *Run) Execute(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (loop.ToolExecutor, error)) (err error) {
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
		at := r.deps.Clock.Now()
		_, _, saveErr := r.AppendEntry(startCtx, sessionentry.Entry{
			ID:      "started:" + r.id.String(),
			Payload: sessionentry.RunStarted{At: at},
		})
		cancel()
		if saveErr != nil {
			err = storageFailure(saveErr)
		} else {
			r.deps.Emitter.Emit(coreevent.Event{
				Source:  r.EventSource(""),
				Payload: coreevent.RunStarted{StartedAt: at},
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

func (r *Run) executeLoop(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (loop.ToolExecutor, error)) error {
	if hookErr := r.deps.HookDispatcher.RunStarted(context.WithoutCancel(ctx), hook.RunEvent{Identity: runIdentity(r, "")}); hookErr != nil {
		r.deps.Logger.Errorf("run start hook failed: %v", hookErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tools, err := prepareTools(ctx)
	if err != nil {
		return err
	}
	params, deps := r.modelRunParams()
	if _, err := runner.Run(ctx, params, deps, r.input, tools); err != nil {
		return err
	}
	if _, _, err := r.AppendEntry(context.WithoutCancel(ctx), sessionentry.Entry{
		ID:      "ending:" + r.id.String(),
		Payload: sessionentry.RunEndingStarted{},
	}); err != nil {
		return err
	}
	r.deps.Emitter.Emit(coreevent.Event{
		Source:  r.EventSource(""),
		Payload: coreevent.RunEnding{},
	})
	return nil
}
