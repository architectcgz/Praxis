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
	"praxis/internal/tools"
	"time"
)

const ErrInterrupted errorcode.Code = "run.interrupted"

// Prepare 提交 Run 和输入事实。返回值表示 RunPrepared 已写入历史。
func (r *Run) Prepare(ctx context.Context, prompt string) (bool, error) {
	prepareCtx, cancel := context.WithTimeout(ctx, r.deps.PrepareTimeout)
	defer cancel()
	ctx = prepareCtx
	if err := r.prepareEntry(ctx); err != nil {
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
	if authorKind == entry.AuthorUser {
		commitCtx, cancelCommit := r.commitContext(ctx)
		defer cancelCommit()
		if hookErr := r.deps.HookDispatcher.UserInputCommitted(commitCtx, hook.EntryCommitted{
			Identity: runIdentity(r, ""),
			Entry:    *stored,
		}); hookErr != nil {
			r.deps.Logger.Errorf("user input committed hook failed: %v", hookErr)
		}
	}
	return true, nil
}

// PrepareReports 直接以真实报告准备主 Run，不创建用户任务或伪造用户输入。
func (r *Run) PrepareReports(ctx context.Context) (bool, error) {
	prepareCtx, cancel := context.WithTimeout(ctx, r.deps.PrepareTimeout)
	defer cancel()
	if err := r.prepareEntry(prepareCtx); err != nil {
		return false, err
	}
	return true, r.acceptReports(prepareCtx)
}

func (r *Run) prepareEntry(ctx context.Context) error {
	_, _, err := r.AppendEntry(ctx, entry.Entry{
		ID:        identity.NewEntryID(),
		CreatedAt: r.createdAt,
		Payload: entry.RunPrepared{
			AgentName:    r.definition.Name,
			DefinitionID: r.definition.ID,
			SourceTaskID: r.sourceTaskID,
		},
	})
	return err
}

// Execute 在本次执行启动或 Loop 结束后提交终态，不接管独立子执行的收尾。
func (r *Run) Execute(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (*tools.Discovery, loop.ToolExecutor, error)) (err error) {
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
		startCtx, cancel := r.commitContext(ctx)
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
	if settleErr := r.settleExecution(ctx, err); settleErr != nil {
		return storageFailure(errors.Join(err, settleErr))
	}
	return err
}

func (r *Run) executeLoop(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (*tools.Discovery, loop.ToolExecutor, error)) error {
	prepareCtx, cancelPrepare := context.WithTimeout(ctx, r.deps.PrepareTimeout)
	defer cancelPrepare()
	if hookErr := r.deps.HookDispatcher.RunStarted(prepareCtx, hook.RunEvent{Identity: runIdentity(r, "")}); hookErr != nil {
		r.deps.Logger.Errorf("run start hook failed: %v", hookErr)
	}
	if err := prepareCtx.Err(); err != nil {
		return err
	}
	discovery, tools, err := prepareTools(prepareCtx)
	prepareErr := prepareCtx.Err()
	cancelPrepare()
	if err != nil {
		return err
	}
	if prepareErr != nil {
		return prepareErr
	}
	params, deps := r.modelRunParams(discovery)
	if _, err := runner.Run(ctx, params, deps, r.input, tools); err != nil {
		return err
	}
	commitCtx, cancel := r.commitContext(ctx)
	defer cancel()
	if _, _, err := r.AppendEntry(commitCtx, entry.Entry{
		ID:      identity.NewEntryID(),
		Payload: entry.RunEndingStarted{},
	}); err != nil {
		return storageFailure(err)
	}
	r.deps.Emitter.Emit(event.Event{
		Source:  r.EventSource(""),
		Payload: event.RunEnding{},
	})
	return nil
}

func (r *Run) commitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), r.deps.CommitTimeout)
}
