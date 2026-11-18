package run

import (
	"context"
	"errors"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	modelapi "praxis/internal/core/model"
	modelusage "praxis/internal/core/model/usage"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"time"
)

type Dependencies struct {
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	Emitter        *coreevent.Emitter
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Config 保存调度准入时确定的执行身份和模型配置。
type Config struct {
	SourceTaskID identity.TaskID
	ParentCallID string
	Definition   agentmodel.AgentDefinition
	Model        modelapi.Config
}

// Run 的可变执行状态由所属流程独占；工具通过消息交接，子任务只交付不可变快照。
// 终态事实提交成功后才发布结果，配置和身份在创建后只读。
type Run struct {
	collaboration collaboration
	toolUpdates   chan toolUpdate
	deps          Dependencies
	history       sessionmodel.AgentHistory

	cancel        context.CancelCauseFunc
	waiting       int
	id            identity.RunID
	sessionID     identity.SessionID
	agentID       identity.AgentID
	definition    agentmodel.AgentDefinition
	model         modelapi.Config
	parentCallID  string
	sourceTaskID  identity.TaskID
	status        sessionentry.RunStatus
	input         *sessionentry.Entry
	createdAt     time.Time
	startedAt     time.Time
	terminal      entryPayload.RunTerminal
	progress      runProgress
	currentTurnID identity.TurnID
	entries       []*sessionentry.Entry
	entryIndexes  map[string]int
}

func New(deps Dependencies, config Config, history sessionmodel.AgentHistory) *Run {
	run := &Run{
		deps:         deps,
		history:      history,
		id:           identity.RunID(deps.IDs.New("run")),
		sessionID:    history.SessionID(),
		agentID:      history.AgentID(),
		sourceTaskID: config.SourceTaskID,
		parentCallID: config.ParentCallID,
		definition:   config.Definition,
		model:        config.Model,
		status:       sessionentry.RunStarting,
		toolUpdates:  make(chan toolUpdate),
		createdAt:    deps.Clock.Now().UTC(),
		entryIndexes: make(map[string]int),
	}
	run.initCollaboration()
	return run
}

// toolUpdate 将并行工具的等待变化和审核事实交给所属执行流程。
// 审核事实必须收到保存确认后才能继续调用模型。
type toolUpdate struct {
	ctx     context.Context
	request sessionentry.ModelRequestRecord
	timing  *timing.Record
	waiting int
	saved   chan error
}

// runProgress 只在本次执行内累计，结束时写入终态载荷。
type runProgress struct {
	Usage            modelusage.Totals
	Iterations       uint64
	ModelRequests    uint64
	OutputBytes      int64
	ToolCalls        int
	FinalStopReason  string
	FinalAssistantID string
}

func (r *Run) Main() bool { return r.parentCallID == "" }

// Snapshot 由所属执行流程在通知和完成交付时调用，接收方只读使用。
func (r *Run) Snapshot() sessionentry.RunSnapshot {
	if r.terminal != nil {
		return sessionentry.SnapshotFromTerminal(r.id, r.terminal)
	}
	return sessionentry.RunSnapshot{
		ID:          r.id,
		Status:      r.status,
		StartedAt:   r.startedAt,
		Iterations:  r.progress.Iterations,
		ToolCalls:   r.progress.ToolCalls,
		OutputBytes: r.progress.OutputBytes,
	}
}

func (r *Run) Entries() []sessionentry.Entry {
	return sessionentry.Values(r.entries)
}

// AppendEntry 保存普通事实；Entry 顺序由所属 Run Loop 决定。
func (r *Run) AppendEntry(ctx context.Context, value sessionentry.Entry) (*sessionentry.Entry, bool, error) {
	value.RunID = r.id.String()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = r.deps.Clock.Now()
	}
	stored, inserted, err := r.history.Append(ctx, value)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return stored, inserted, err
		}
		return stored, inserted, storageFailure(err)
	}
	if inserted {
		r.accept(stored)
	}
	return stored, inserted, nil
}

// accept 只消费已经持久化成功的事实，并推进运行时状态。
func (r *Run) accept(value *sessionentry.Entry) {
	r.entryIndexes[value.ID] = len(r.entries)
	r.entries = append(r.entries, value)
	switch p := value.Payload.(type) {
	case entryPayload.RunStarted:
		r.status = sessionentry.RunRunning
		r.startedAt = p.At.UTC()
	case entryPayload.RunEndingStarted:
		r.status = sessionentry.RunEnding
	case entryPayload.TurnStarted:
		r.currentTurnID = identity.TurnID(value.TurnID)
		r.progress.Iterations++
	case entryPayload.ModelRequestStarted:
		r.progress.ModelRequests++
	}
	if terminal, ok := value.Payload.(entryPayload.RunTerminal); ok {
		r.terminal = terminal
	} else {
		r.progress.accept(value)
	}
	if parent := r.collaboration.parent; parent != nil {
		parent.ReceiveSubagentStatus(r.agentID, r.Snapshot())
	}
}

func (p *runProgress) accept(value *sessionentry.Entry) {
	if request, _ := value.RequestFact(); request != nil && request.Status != sessionentry.RequestRunning {
		p.OutputBytes += int64(request.OutputBytes)
		if u := request.Usage; u != nil {
			p.Usage.Add(*u)
		}
	}
	if value.Role == role.Assistant {
		p.FinalAssistantID, p.FinalStopReason = "", ""
		if value.Response != nil && (value.Response.StopReason == "completed" || value.Response.StopReason == "stop_sequence") {
			p.FinalAssistantID, p.FinalStopReason = value.ID, value.Response.StopReason
		}
	}
	for _, block := range value.Content {
		switch block := block.(type) {
		case sessionentry.ToolCallBlock:
			p.ToolCalls++
			p.FinalAssistantID, p.FinalStopReason = "", ""
		case sessionentry.ToolResultBlock:
			p.OutputBytes += int64(len(block.Text))
		}
	}
}

func (r *Run) EventSource(turnID identity.TurnID) coreevent.EventSource {
	return coreevent.EventSource{
		SessionID: r.sessionID,
		AgentID:   r.agentID,
		RunID:     r.id,
		TurnID:    turnID,
	}
}

// BindCancellation 由 Coordinator 在执行准备时绑定，供所属子执行报告无法保存等失败直接停止本次执行。
func (r *Run) BindCancellation(cancel context.CancelCauseFunc) { r.cancel = cancel }
func (r *Run) Cancel(cause error)                              { r.cancel(cause) }

func (r *Run) BeginWaiting() { r.toolUpdates <- toolUpdate{waiting: 1} }
func (r *Run) EndWaiting()   { r.toolUpdates <- toolUpdate{waiting: -1} }

func (r *Run) TerminalSaved() bool { return r.terminal != nil }

// executeTools 在所属执行流程接纳工具消息，直到本批工具及其清理全部完成。
// 取消通过工具 Context 传播，此处仍接纳收尾事实，不提前退出等待。
func (r *Run) executeTools(execute func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		execute()
	}()
	for {
		select {
		case <-done:
			return
		case update := <-r.toolUpdates:
			if update.waiting != 0 {
				r.waiting += update.waiting
				if r.waiting == 0 || update.waiting > 0 && r.waiting == 1 {
					state := agentmodel.AgentExecuting
					if r.waiting > 0 {
						state = agentmodel.AgentWaiting
					}
					r.deps.Emitter.Emit(coreevent.Event{
						Source:  r.EventSource(""),
						Payload: coreevent.AgentStateChanged{State: state},
					})
				}
				continue
			}
			update.saved <- r.commitToolRequest(update)
		}
	}
}

func (r *Run) ID() identity.RunID {
	return r.id
}

func (r *Run) SessionID() identity.SessionID {
	return r.sessionID
}

func (r *Run) AgentID() identity.AgentID {
	return r.agentID
}

func (r *Run) CreatedAt() time.Time {
	return r.createdAt
}

func (r *Run) SourceTaskID() identity.TaskID {
	return r.sourceTaskID
}

func (r *Run) InputText() string {
	return r.input.TextContent()
}

func (r *Run) Model() modelapi.Config {
	return r.model
}

func (r *Run) Outcome() entryPayload.RunOutcome {
	if r.terminal == nil {
		return ""
	}
	return r.terminal.Outcome()
}

// RecordModelRequest 将审核事实交给所属执行流程，并等待保存结果。
func (r *Run) RecordModelRequest(ctx context.Context, request sessionentry.ModelRequestRecord, measured *timing.Record) error {
	update := toolUpdate{
		ctx:     ctx,
		request: request,
		timing:  measured,
		saved:   make(chan error, 1),
	}
	select {
	case r.toolUpdates <- update:
		return <-update.saved
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Run) commitToolRequest(update toolUpdate) error {
	request, measured := update.request, update.timing
	stored, _, err := r.AppendEntry(update.ctx, sessionentry.ModelRequestEntry(request, measured))
	if err != nil {
		return err
	}
	r.deps.Emitter.Emit(coreevent.Event{
		Source:  r.EventSource(request.TurnID),
		Payload: coreevent.ModelRequestFact(*stored),
	})
	return nil
}

func (s *Run) modelRunParams() (loop.RunParams, loop.RunDependencies) {
	return loop.RunParams{
			ID:           s.id,
			SourceTaskID: s.sourceTaskID,
			SessionID:    s.sessionID,
			AgentID:      s.agentID,
			SystemPrompt: s.definition.Instructions,
			Model:        s.model,
		}, loop.RunDependencies{
			AcceptSubagentReports: s.acceptReports,
			AcceptInstructions:    s.acceptInstructions,
			FinishInstructions:    s.finishInstructions,
			Budget:                &appcontext.Budget{},
			ReadEntries:           s.loopEntries,
			AppendEntry:           s.AppendEntry,
			ExecuteTools:          s.executeTools,
		}
}

func (s *Run) loopEntries(ctx context.Context) ([]*sessionentry.Entry, error) {
	entries, err := s.history.ModelEntries(ctx)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil, err
		}
		return nil, storageFailure(err)
	}
	return entries, nil
}

func runIdentity(run *Run, turnID identity.TurnID) hook.Identity {
	return hook.Identity{
		SourceTaskID: run.sourceTaskID,
		SessionID:    run.sessionID,
		AgentID:      run.agentID,
		RunID:        run.id,
		TurnID:       turnID,
	}
}

func (s *Run) emitError(turnID identity.TurnID, err error) {
	message := "input failed"
	if failure, ok := errors.AsType[*loop.Failure](err); ok {
		if failure.Message() != "" {
			message = failure.Message()
		} else if loop.FailureClassification(err) == loop.FailureStorage {
			message = "Failed to save session entries."
		} else if loop.FailureClassification(err) == loop.FailureResourceLimit {
			message = "model context window exceeded"
		} else if loop.FailureClassification(err) == loop.FailureProvider {
			text := []rune(err.Error())
			if len(text) > 4096 {
				text = append(text[:4096], '.', '.', '.')
			}
			message = string(text)
		}
	}
	s.deps.Emitter.Emit(coreevent.Event{
		Source:  coreevent.SourceForRun(s, turnID),
		Payload: coreevent.ErrorOccurred{Message: message},
	})
}

func storageFailure(err error) error {
	return loop.NewFailure(loop.FailureStorage, err)
}
