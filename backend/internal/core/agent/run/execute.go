package run

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"strings"
	"time"
)

const ErrInterrupted errorcode.Code = "run.interrupted"

type SubagentInput struct {
	Task         taskmodel.Task
	AgentID      identity.AgentID
	ParentRunID  identity.RunID
	ParentCallID string
	Definition   agentmodel.AgentDefinition
	Model        modelapi.Config
	Owner        SessionContext
}

func PrepareTask(ctx context.Context, deps Dependencies, input taskmodel.Task, agentID identity.AgentID, definition agentmodel.AgentDefinition, model modelapi.Config, owner SessionContext) (*Run, error) {
	run, err := prepare(ctx, deps, input, agentID, "", "", definition, model, owner)
	return run, err
}

func PrepareSubagent(ctx context.Context, deps Dependencies, input SubagentInput) (*Run, error) {
	if err := input.Owner.CreateSubagent(ctx, input.AgentID); err != nil {
		return nil, err
	}
	return prepare(ctx, deps, input.Task, input.AgentID, input.ParentRunID, input.ParentCallID, input.Definition, input.Model, input.Owner)
}

func prepare(ctx context.Context, deps Dependencies, input taskmodel.Task, agentID identity.AgentID, parentID identity.RunID, parentCall string, definition agentmodel.AgentDefinition, model modelapi.Config, owner SessionContext) (*Run, error) {
	run := &Run{
		main:          parentID == "",
		Session:       owner,
		deps:          deps,
		id:            identity.RunID(deps.IDs.New("run")),
		sessionID:     input.SessionID,
		agentID:       agentID,
		definitionID:  definition.ID,
		systemPrompt:  definition.Instructions,
		model:         model,
		parentRunID:   parentID,
		parentCallID:  parentCall,
		sourceTaskID:  input.ID,
		status:        sessionentry.RunStarting,
		inputEntryID:  "input:" + input.ID.String(),
		inputText:     input.Prompt,
		toolUpdates:   make(chan toolUpdate),
		createdAt:     deps.Clock.Now().UTC(),
		entryIndexes:  make(map[string]int),
		appendFact:    owner.BindHistory(agentID),
	}
	run.initCollaboration()
	if _, _, err := run.AppendEntry(ctx, sessionentry.Entry{
		ID:        "prepared:" + run.id.String(),
		CreatedAt: run.createdAt,
		Payload: sessionentry.RunPrepared{
			AgentName:    definition.Name,
			DefinitionID: definition.ID,
			SourceTaskID: input.ID,
			InputEntryID: run.inputEntryID,
		},
	}); err != nil {
		return run, err
	}
	stored, _, err := run.AppendEntry(ctx, sessionentry.Entry{
		ID: run.inputEntryID,
		Payload: sessionentry.UserInput{
			MessageID: run.inputEntryID,
			Text:      input.Prompt,
		},
		Role:       role.User,
		AuthorKind: sessionentry.AuthorUser,
		Content:    []sessionentry.ContentBlock{sessionentry.TextBlock{Text: input.Prompt}},
	})
	if err != nil {
		return run, err
	}
	deps.Emitter.Emit(coreevent.Event{
		Source:  run.EventSource(""),
		Payload: coreevent.EntriesAppended{Entries: []sessionentry.Entry{*stored}},
	})
	if hookErr := deps.HookDispatcher.UserInputCommitted(context.WithoutCancel(ctx), hook.EntryCommitted{
		Identity: runIdentity(run, ""),
		Entry:    *stored,
	}); hookErr != nil {
		deps.Logger.Errorf("user input committed hook failed: %v", hookErr)
	}
	return run, nil
}

// Execute 覆盖本次准备、完整 Loop 和必要收尾；调用方只负责执行位置和后续调度。
func (r *Run) Execute(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (loop.ToolExecutor, error)) (err error) {
	if r.status != sessionentry.RunStarting || r.result != nil {
		return errorcode.ErrInvalidTransition
	}
	ctx, scope := r.deps.HookDispatcher.Start(ctx, hook.Operation{
		Identity: runIdentity(r, ""),
		Kind:     hook.OperationAgent,
		Name:     r.definitionID.String(),
	})
	defer func() {
		scope.End(hook.Result{
			Outcome: string(r.Outcome()),
			Err:     err,
		})
	}()
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	err = r.ConfirmRunning(startCtx)
	cancel()
	if err == nil {
		if hookErr := r.deps.HookDispatcher.RunStarted(context.WithoutCancel(ctx), hook.RunEvent{Identity: runIdentity(r, "")}); hookErr != nil {
			r.deps.Logger.Errorf("run start hook failed: %v", hookErr)
		}
		if err = ctx.Err(); err == nil {
			var tools loop.ToolExecutor
			tools, err = prepareTools(ctx)
			if err == nil {
				input, _ := r.Entry(r.inputEntryID)
				params, deps := r.modelRunParams()
				_, err = runner.Run(ctx, params, deps, input, tools)
			}
		}
	}
	if err == nil {
		if _, _, saveErr := r.AppendEntry(context.WithoutCancel(ctx), sessionentry.Entry{
			ID:      "ending:" + r.id.String(),
			Payload: sessionentry.RunEndingStarted{},
		}); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
	}
	r.finishChildren()
	outcome, code, message := r.classifyOutcome(ctx, err)
	if !r.Main() && outcome == sessionentry.OutcomeCompleted {
		outcome = sessionentry.OutcomeYielded
	}
	if err != nil {
		r.emitError(r.currentTurnID, err)
	}
	// 收尾不继承已取消的执行 Context，保存失败明确返回，不自动重投。
	endCtx, cancelEnd := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelEnd()
	if closeErr := r.closeToolCalls(endCtx); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	saveErr := r.Settle(endCtx, outcome, code, message)
	return errors.Join(err, saveErr)
}

func (r *Run) ConfirmRunning(ctx context.Context) error {
	at := r.deps.Clock.Now()
	_, _, err := r.AppendEntry(ctx, sessionentry.Entry{
		ID:      "started:" + r.id.String(),
		Payload: sessionentry.RunStarted{At: at},
	})
	if err == nil {
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(""),
			Payload: coreevent.RunStarted{Run: r.Snapshot()},
		})
	}
	return err
}

func (r *Run) Settle(ctx context.Context, outcome sessionentry.RunOutcome, code sessionentry.RunFailureCode, message string) error {
	result := sessionentry.RunResult{
		Outcome:          string(outcome),
		FailureCode:      string(code),
		FailureMessage:   message,
		StartedAt:        r.startedAt,
		EndedAt:          r.deps.Clock.Now(),
		Usage:            r.progress.Usage,
		ModelRequests:    r.progress.ModelRequests,
		Iterations:       r.progress.Iterations,
		OutputBytes:      r.progress.OutputBytes,
		ToolCalls:        r.progress.ToolCalls,
		FinalAssistantID: r.progress.FinalAssistantID,
		FinalStopReason:  r.progress.FinalStopReason,
	}
	var payload sessionentry.Payload
	switch outcome {
	case sessionentry.OutcomeCompleted:
		payload = sessionentry.RunCompleted{Result: result}
	case sessionentry.OutcomeYielded:
		payload = sessionentry.SubagentYielded{Result: result}
	case sessionentry.OutcomeFailed:
		payload = sessionentry.RunFailed{Result: result}
	case sessionentry.OutcomeInterrupted:
		payload = sessionentry.RunInterrupted{Result: result}
	}
	stored, err := r.Session.CommitTerminal(ctx, r.agentID, sessionentry.Entry{
		ID:      "run:" + r.id.String(),
		RunID:   r.id.String(),
		Payload: payload,
	})
	if err == nil {
		r.accept(stored)
		if hookErr := r.deps.HookDispatcher.RunCommitted(context.WithoutCancel(ctx), hook.RunEvent{
			Identity:    runIdentity(r, ""),
			Outcome:     string(outcome),
			FailureCode: string(code),
		}); hookErr != nil {
			r.deps.Logger.Errorf("run committed hook failed: %v", hookErr)
		}
		if r.Main() && outcome == sessionentry.OutcomeCompleted {
			r.nameSession(ctx)
		}
	}
	if err == nil {
		snapshot := r.Snapshot()
		r.deps.Emitter.Emit(coreevent.Event{
			Source: r.EventSource(""),
			Payload: coreevent.RunEnded{
				Run:     snapshot,
				State:   r.result.AgentState(),
				Entries: []sessionentry.Entry{*stored},
			},
		})
	}
	return err
}

func (r *Run) nameSession(ctx context.Context) {
	info, err := r.deps.Sessions.Get(ctx, r.sessionID)
	if err != nil || info.Title != "" {
		return
	}
	input, ok := r.Entry(r.inputEntryID)
	if !ok {
		return
	}
	title := sessionmodel.TitleFromInput(input.TextContent())
	if title != "" {
		if err := r.deps.Sessions.UpdateTitle(ctx, info.ID, info.Title, title, r.deps.Clock.Now()); err != nil {
			r.deps.Logger.Errorf("name session: %v", err)
		}
	}
}

func (r *Run) classifyOutcome(ctx context.Context, err error) (sessionentry.RunOutcome, sessionentry.RunFailureCode, string) {
	if loop.FailureClassification(context.Cause(ctx)) == loop.FailureStorage {
		return sessionentry.OutcomeFailed, sessionentry.RunFailureStorage, ""
	}
	if err == nil {
		return sessionentry.OutcomeCompleted, "", ""
	}
	code := failureCode(err)
	if errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		return sessionentry.OutcomeFailed, sessionentry.RunFailureTimeout, ""
	}
	if code != sessionentry.RunFailureStorage && ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, context.Cause(ctx))) {
		code = sessionentry.RunFailureCancelled
		if errors.Is(context.Cause(ctx), ErrInterrupted) {
			code = sessionentry.RunFailureRequestCanceled
		}
		return sessionentry.OutcomeInterrupted, code, ""
	}
	message := ""
	if code == sessionentry.RunFailureProvider {
		text := []rune(strings.TrimSpace(err.Error()))
		if len(text) > sessionentry.MaxRunFailureMessageRunes {
			text = text[:sessionentry.MaxRunFailureMessageRunes]
		}
		message = string(text)
	}
	return sessionentry.OutcomeFailed, code, message
}

func failureCode(err error) sessionentry.RunFailureCode {
	switch loop.FailureClassification(err) {
	case loop.FailureProviderUnavailable:
		return sessionentry.RunFailureProviderUnavailable
	case loop.FailureProvider:
		return sessionentry.RunFailureProvider
	case loop.FailureTool:
		return sessionentry.RunFailureTool
	case loop.FailurePolicyBlocked:
		return sessionentry.RunFailurePolicyBlocked
	case loop.FailureResourceLimit:
		return sessionentry.RunFailureResourceLimit
	case loop.FailureStorage:
		return sessionentry.RunFailureStorage
	case loop.FailureContract:
		return sessionentry.RunFailureContract
	case loop.FailureBusy:
		return sessionentry.RunFailureBusy
	default:
		return sessionentry.RunFailureInternal
	}
}
