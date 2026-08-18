package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
	"praxis/internal/core/runtime"
	"praxis/internal/core/system"
)

// QueueTaskRequest contains the already approved inputs for one independent
// task. The scheduler persists these values before it considers starting it.
type QueueTaskRequest struct {
	TaskSessionID   domain.TaskSessionID
	AgentThreadID   domain.AgentThreadID
	Prompt          string
	TaskPacket      domain.TaskPacket
	ContextManifest domain.ContextManifest
	Grant           domain.CapabilityGrant
	Execution       domain.RuntimeExecutionSnapshot
	CreatedAt       time.Time
}

// StartResult describes a work item that was durably claimed and associated
// with an AgentRun. Started is false when the thread was not eligible or its
// queue had no item to claim.
type StartResult struct {
	Started  bool
	WorkItem domain.QueuedWorkItem
	Run      domain.AgentRun
	Lease    domain.WorkspaceWriteLease
}

// QueueSchedulerConfig supplies the durable collaborators and runtime factory
// needed to operate one task-level queue.
type QueueSchedulerConfig struct {
	Transactions persistence.TxRunner
	Queue        persistence.WorkQueueRepository
	Threads      persistence.AgentThreadRepository
	Runs         persistence.AgentRunRepository
	Packets      persistence.TaskPacketRepository
	Manifests    persistence.ContextManifestRepository
	Grants       persistence.CapabilityGrantRepository
	Leases       persistence.WorkspaceLeaseRepository
	Events       persistence.EventRepository
	Runtime      runtime.RuntimeFactory
	Clock        system.Clock

	// LifecycleContext is independent of an individual binding request so a
	// queued runtime can continue after the enqueue call returns.
	LifecycleContext context.Context
}

// QueueScheduler owns durable task-level scheduling for AgentThreads. It does
// not implement the runtime's steer/follow-up queues.
type QueueScheduler struct {
	tx               persistence.TxRunner
	queue            persistence.WorkQueueRepository
	threads          persistence.AgentThreadRepository
	runs             persistence.AgentRunRepository
	packets          persistence.TaskPacketRepository
	manifests        persistence.ContextManifestRepository
	grants           persistence.CapabilityGrantRepository
	leases           persistence.WorkspaceLeaseRepository
	events           persistence.EventRepository
	runtime          runtime.RuntimeFactory
	clock            system.Clock
	lifecycleContext context.Context

	guardMu sync.Mutex
	guards  map[domain.AgentThreadID]*sync.Mutex

	runtimeMu      sync.Mutex
	current        map[domain.AgentThreadID]runtimeEntry
	retired        []runtime.Runtime
	nextGeneration uint64
	shuttingDown   bool
}

type runtimeEntry struct {
	generation uint64
	runtime    runtime.Runtime
}

type startPlan struct {
	thread   domain.AgentThread
	item     domain.QueuedWorkItem
	run      domain.AgentRun
	packet   domain.TaskPacket
	manifest domain.ContextManifest
	grant    domain.CapabilityGrant
	lease    domain.WorkspaceWriteLease
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// NewQueueScheduler validates and constructs a task-level scheduler.
func NewQueueScheduler(config QueueSchedulerConfig) (*QueueScheduler, error) {
	missing := func(name string, value any) error {
		if value == nil {
			return fmt.Errorf("queue scheduler %s is required", name)
		}
		return nil
	}
	for _, required := range []struct {
		name  string
		value any
	}{
		{"transactions", config.Transactions},
		{"queue", config.Queue},
		{"threads", config.Threads},
		{"runs", config.Runs},
		{"packets", config.Packets},
		{"manifests", config.Manifests},
		{"grants", config.Grants},
		{"leases", config.Leases},
		{"runtime", config.Runtime},
	} {
		if err := missing(required.name, required.value); err != nil {
			return nil, err
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	}
	lifecycleContext := config.LifecycleContext
	if lifecycleContext == nil {
		lifecycleContext = context.Background()
	}
	return &QueueScheduler{
		tx: config.Transactions, queue: config.Queue, threads: config.Threads,
		runs: config.Runs, packets: config.Packets, manifests: config.Manifests,
		grants: config.Grants, leases: config.Leases, events: config.Events,
		runtime: config.Runtime, clock: clock, lifecycleContext: lifecycleContext,
		guards:  make(map[domain.AgentThreadID]*sync.Mutex),
		current: make(map[domain.AgentThreadID]runtimeEntry),
	}, nil
}

// QueueTask durably appends an approved independent task. An idle thread uses
// the same claim path as a settled thread; a running or blocked thread only
// receives the durable queue item and keeps its current runtime unchanged.
func (s *QueueScheduler) QueueTask(ctx context.Context, request QueueTaskRequest) (domain.QueuedWorkItem, error) {
	if ctx == nil {
		return domain.QueuedWorkItem{}, errors.New("queue task context is required")
	}
	if s.isShuttingDown() {
		return domain.QueuedWorkItem{}, errors.New("queue scheduler is shutting down")
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = s.clock.Now()
	}
	guard := s.threadGuard(request.AgentThreadID)
	guard.Lock()
	var item domain.QueuedWorkItem
	var startImmediately bool
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		thread, err := s.threads.Get(txCtx, request.AgentThreadID)
		if err != nil {
			return err
		}
		if err := validateQueueRequest(request, thread); err != nil {
			return err
		}
		if thread.State == domain.ThreadClosed {
			return invalidSchedulerState("agent thread is closed")
		}
		if err := s.packets.Save(txCtx, request.TaskPacket); err != nil {
			return err
		}
		if err := s.manifests.Save(txCtx, request.ContextManifest); err != nil {
			return err
		}
		if err := s.grants.Save(txCtx, request.Grant.Snapshot()); err != nil {
			return err
		}
		sequence, err := s.queue.NextSequence(txCtx, request.AgentThreadID)
		if err != nil {
			return fmt.Errorf("allocate queued work item: %w", err)
		}
		item, err = domain.NewQueuedWorkItem(
			domain.NewWorkItemID(), request.TaskSessionID, request.AgentThreadID,
			sequence, request.Prompt, request.TaskPacket.ID, request.ContextManifest.ID,
			request.Grant.ID, request.Execution, request.CreatedAt,
		)
		if err != nil {
			return err
		}
		if err := s.queue.Enqueue(txCtx, item); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, queuedEvent(item, request.CreatedAt)); err != nil {
			return err
		}
		startImmediately = thread.State == domain.ThreadIdle
		return nil
	})
	guard.Unlock()
	if err != nil {
		return domain.QueuedWorkItem{}, err
	}
	if startImmediately {
		result, err := s.StartNextQueued(ctx, request.AgentThreadID)
		if result.Started {
			item = result.WorkItem
		}
		if err != nil {
			return item, fmt.Errorf("start queued work item: %w", err)
		}
	}
	return item, nil
}

// EnqueueWorkItem is the explicit spelling of QueueTask for command handlers.
func (s *QueueScheduler) EnqueueWorkItem(ctx context.Context, request QueueTaskRequest) (domain.QueuedWorkItem, error) {
	return s.QueueTask(ctx, request)
}

// StartNextQueued claims and starts the earliest queued item when the thread
// is idle. The claim and product records are committed before runtime I/O.
func (s *QueueScheduler) StartNextQueued(ctx context.Context, threadID domain.AgentThreadID) (StartResult, error) {
	if ctx == nil {
		return StartResult{}, errors.New("start queued context is required")
	}
	guard := s.threadGuard(threadID)
	guard.Lock()
	plan, result, err := s.claimNext(ctx, threadID)
	guard.Unlock()
	if err != nil || !result.Started {
		return result, err
	}
	if err := s.startRuntime(ctx, plan); err != nil {
		return result, err
	}
	return result, nil
}

// OnSettle consumes one runtime settlement. Duplicate callbacks for an
// already settled AgentRun are idempotent and cannot claim another item.
func (s *QueueScheduler) OnSettle(ctx context.Context, settlement runtime.Settlement) error {
	if ctx == nil {
		return errors.New("settlement context is required")
	}
	if settlement.RunID == "" || settlement.ThreadID == "" {
		return errors.New("settlement run and thread are required")
	}
	guard := s.threadGuard(settlement.ThreadID)
	guard.Lock()
	var next *startPlan
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		run, err := s.runs.Get(txCtx, settlement.RunID)
		if err != nil {
			return err
		}
		if run.AgentThreadID != settlement.ThreadID {
			return invalidSchedulerState("settlement thread does not own the run")
		}
		if !run.SettledAt.IsZero() {
			return nil
		}
		if run.WorkItemID == "" {
			return invalidSchedulerState("product settlement requires a work item")
		}
		item, err := s.queue.Get(txCtx, run.WorkItemID)
		if err != nil {
			return err
		}
		thread, err := s.threads.Get(txCtx, settlement.ThreadID)
		if err != nil {
			return err
		}
		if thread.CurrentRunID != run.ID || item.AgentRunID != run.ID || item.Status != domain.WorkItemRunning {
			return invalidSchedulerState("settlement is not for the active work item")
		}
		grant, err := s.grants.Get(txCtx, item.CapabilityGrantID)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		if settlement.Outcome == domain.RunPaused && thread.State == domain.ThreadRunning {
			if _, err := thread.RequestPause(at); err != nil {
				return err
			}
		}
		runEvent, err := run.Settle(settlement.Outcome, at, settlement.FailureCode)
		if err != nil {
			return err
		}
		itemEvent, err := item.Settle(run.ID, settlement.Outcome, at, settlement.FailureCode)
		if err != nil {
			return err
		}
		threadEvent, err := thread.Settle(settlement.Outcome, at)
		if err != nil {
			return err
		}
		if err := s.runs.Save(txCtx, run); err != nil {
			return err
		}
		if err := s.queue.Save(txCtx, item); err != nil {
			return err
		}
		if err := s.threads.Save(txCtx, thread); err != nil {
			return err
		}
		leaseEvent, err := s.releaseLease(txCtx, grant, thread.ID, at)
		if err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, runEvent, itemEvent, threadEvent, leaseEvent); err != nil {
			return err
		}
		if settlement.Outcome != domain.RunCompleted || s.isShuttingDown() {
			return nil
		}
		candidate, started, err := s.claimNextInTx(txCtx, thread.ID, at)
		if err != nil {
			if errors.Is(err, domain.ErrWorkQueueEmpty) {
				return nil
			}
			return err
		}
		if started {
			next = &candidate
		}
		return nil
	})
	guard.Unlock()
	if err != nil {
		return err
	}
	if next == nil {
		return nil
	}
	return s.startRuntime(ctx, *next)
}

// ResumeAgent explicitly resumes the current paused or interrupted work item
// with a fresh AgentRun and the item's original execution snapshot.
func (s *QueueScheduler) ResumeAgent(ctx context.Context, threadID domain.AgentThreadID) (StartResult, error) {
	if ctx == nil {
		return StartResult{}, errors.New("resume context is required")
	}
	guard := s.threadGuard(threadID)
	guard.Lock()
	var plan startPlan
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		thread, err := s.threads.Get(txCtx, threadID)
		if err != nil {
			return err
		}
		if thread.State != domain.ThreadPaused && thread.State != domain.ThreadInterrupted {
			return invalidSchedulerState("only a paused or interrupted thread can resume")
		}
		if thread.CurrentRunID == "" {
			return invalidSchedulerState("thread has no run to resume")
		}
		previousRun, err := s.runs.Get(txCtx, thread.CurrentRunID)
		if err != nil {
			return err
		}
		if previousRun.WorkItemID == "" {
			return invalidSchedulerState("resume requires a work item run")
		}
		item, err := s.queue.Get(txCtx, previousRun.WorkItemID)
		if err != nil {
			return err
		}
		if item.Status != domain.WorkItemPaused && item.Status != domain.WorkItemInterrupted {
			return invalidSchedulerState("current work item is not resumable")
		}
		plan, err = s.materializePlan(txCtx, thread, item, domain.NewAgentRunID(), "work_item_resume", s.clock.Now())
		if err != nil {
			return err
		}
		itemEvent, err := item.Resume(plan.run.ID, plan.run.StartedAt)
		if err != nil {
			return err
		}
		threadEvent, err := thread.Start(plan.run.ID, plan.run.StartedAt)
		if err != nil {
			return err
		}
		leaseEvent, err := s.acquireLease(txCtx, plan.grant, thread.ID, plan.run.StartedAt)
		if err != nil {
			return err
		}
		plan.item = item
		plan.thread = thread
		plan.lease = leaseEvent.lease
		if err := s.queue.Save(txCtx, item); err != nil {
			return err
		}
		if err := s.runs.Save(txCtx, plan.run); err != nil {
			return err
		}
		if err := s.threads.Save(txCtx, thread); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, itemEvent, threadEvent, leaseEvent.event); err != nil {
			return err
		}
		return nil
	})
	guard.Unlock()
	if err != nil {
		return StartResult{}, err
	}
	result := StartResult{Started: true, WorkItem: plan.item, Run: plan.run, Lease: plan.lease}
	if err := s.startRuntime(ctx, plan); err != nil {
		return result, err
	}
	return result, nil
}

// Shutdown closes managed runtimes and prevents new queue mutations. Runtime
// close callbacks still settle durable state, but cannot auto-claim successors.
func (s *QueueScheduler) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("scheduler shutdown context is required")
	}
	s.runtimeMu.Lock()
	s.shuttingDown = true
	entries := make([]runtimeEntry, 0, len(s.current))
	for threadID, entry := range s.current {
		entries = append(entries, entry)
		delete(s.current, threadID)
	}
	retired := append([]runtime.Runtime(nil), s.retired...)
	s.retired = nil
	s.runtimeMu.Unlock()
	var firstErr error
	for _, entry := range entries {
		if err := entry.runtime.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, runtime := range retired {
		if err := runtime.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *QueueScheduler) claimNext(ctx context.Context, threadID domain.AgentThreadID) (startPlan, StartResult, error) {
	var plan startPlan
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		candidate, started, err := s.claimNextInTx(txCtx, threadID, s.clock.Now())
		if err != nil {
			if errors.Is(err, domain.ErrWorkQueueEmpty) {
				return nil
			}
			return err
		}
		if started {
			plan = candidate
		}
		return nil
	})
	if err != nil {
		return startPlan{}, StartResult{}, err
	}
	if plan.run.ID == "" {
		return startPlan{}, StartResult{}, nil
	}
	return plan, StartResult{Started: true, WorkItem: plan.item, Run: plan.run, Lease: plan.lease}, nil
}

func (s *QueueScheduler) claimNextInTx(ctx context.Context, threadID domain.AgentThreadID, at time.Time) (startPlan, bool, error) {
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return startPlan{}, false, err
	}
	if thread.State != domain.ThreadIdle {
		return startPlan{}, false, nil
	}
	plan, err := s.materializeClaim(ctx, thread, at)
	if err != nil {
		return startPlan{}, false, err
	}
	threadEvent, err := thread.Start(plan.run.ID, at)
	if err != nil {
		return startPlan{}, false, err
	}
	leaseEvent, err := s.acquireLease(ctx, plan.grant, thread.ID, at)
	if err != nil {
		return startPlan{}, false, err
	}
	plan.thread = thread
	plan.lease = leaseEvent.lease
	if err := s.runs.Save(ctx, plan.run); err != nil {
		return startPlan{}, false, err
	}
	if err := s.threads.Save(ctx, thread); err != nil {
		return startPlan{}, false, err
	}
	if err := s.appendEvent(ctx, startedEvent(plan.item, at), threadEvent, leaseEvent.event); err != nil {
		return startPlan{}, false, err
	}
	return plan, true, nil
}

func (s *QueueScheduler) materializeClaim(ctx context.Context, thread domain.AgentThread, at time.Time) (startPlan, error) {
	runID := domain.NewAgentRunID()
	item, err := s.queue.ClaimNext(ctx, thread.ID, runID, at)
	if err != nil {
		return startPlan{}, err
	}
	return s.materializePlan(ctx, thread, item, runID, "queued_work_item", at)
}

func (s *QueueScheduler) materializePlan(ctx context.Context, thread domain.AgentThread, item domain.QueuedWorkItem, runID domain.AgentRunID, reason string, at time.Time) (startPlan, error) {
	packet, err := s.packets.Get(ctx, item.TaskPacketID)
	if err != nil {
		return startPlan{}, err
	}
	manifest, err := s.manifests.Get(ctx, item.ContextManifestID)
	if err != nil {
		return startPlan{}, err
	}
	grant, err := s.grants.Get(ctx, item.CapabilityGrantID)
	if err != nil {
		return startPlan{}, err
	}
	if err := validateMaterializedItem(thread, item, packet, manifest, grant); err != nil {
		return startPlan{}, err
	}
	run, err := domain.NewAgentRunForWorkItem(runID, item.ID, thread.ID, reason, item.Execution, at)
	if err != nil {
		return startPlan{}, err
	}
	return startPlan{thread: thread, item: item, run: run, packet: packet, manifest: manifest, grant: grant}, nil
}

func (s *QueueScheduler) startRuntime(ctx context.Context, plan startPlan) error {
	config := runtime.RuntimeConfig{
		Thread: plan.thread, TaskPacket: plan.packet, ContextManifest: plan.manifest,
		Grant: plan.grant, Execution: plan.item.Execution, Model: plan.grant.Model,
		SessionReference: plan.thread.RuntimeSessionRef, LeaseReference: plan.lease.ID.String(),
		OnSettle: s.OnSettle,
	}
	runtime, err := s.runtime.New(s.lifecycleContext, config)
	if err != nil {
		return s.failRuntimeStart(ctx, plan, err)
	}
	if runtime == nil {
		return s.failRuntimeStart(ctx, plan, errors.New("runtime factory returned nil runtime"))
	}
	generation := s.registerRuntime(plan.thread.ID, runtime)
	if err := runtime.Start(s.lifecycleContext, plan.run, plan.item.Prompt); err != nil {
		s.unregisterRuntime(plan.thread.ID, generation)
		_ = runtime.Close(context.WithoutCancel(s.lifecycleContext))
		return s.failRuntimeStart(ctx, plan, err)
	}
	return nil
}

func (s *QueueScheduler) failRuntimeStart(ctx context.Context, plan startPlan, cause error) error {
	compensationErr := s.compensateRuntimeFailure(ctx, plan, "runtime_start_failed")
	if compensationErr != nil {
		return fmt.Errorf("runtime start failed: %v; compensation failed: %w", cause, compensationErr)
	}
	return fmt.Errorf("runtime start failed: %w", cause)
}

func (s *QueueScheduler) compensateRuntimeFailure(ctx context.Context, plan startPlan, failureCode string) error {
	return s.tx.InTx(s.lifecycleContext, func(txCtx context.Context) error {
		run, err := s.runs.Get(txCtx, plan.run.ID)
		if err != nil {
			return err
		}
		if !run.SettledAt.IsZero() {
			return nil
		}
		item, err := s.queue.Get(txCtx, plan.item.ID)
		if err != nil {
			return err
		}
		thread, err := s.threads.Get(txCtx, plan.thread.ID)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		runEvent, err := run.Settle(domain.RunFailed, at, failureCode)
		if err != nil {
			return err
		}
		itemEvent, err := item.Settle(run.ID, domain.RunFailed, at, failureCode)
		if err != nil {
			return err
		}
		threadEvent, err := thread.Settle(domain.RunFailed, at)
		if err != nil {
			return err
		}
		grant, err := s.grants.Get(txCtx, item.CapabilityGrantID)
		if err != nil {
			return err
		}
		if err := s.runs.Save(txCtx, run); err != nil {
			return err
		}
		if err := s.queue.Save(txCtx, item); err != nil {
			return err
		}
		if err := s.threads.Save(txCtx, thread); err != nil {
			return err
		}
		leaseEvent, err := s.releaseLease(txCtx, grant, thread.ID, at)
		if err != nil {
			return err
		}
		return s.appendEvent(txCtx, runEvent, itemEvent, threadEvent, leaseEvent)
	})
}

type leaseAcquisition struct {
	lease domain.WorkspaceWriteLease
	event domain.DomainEvent
}

func (s *QueueScheduler) acquireLease(ctx context.Context, grant domain.CapabilityGrant, threadID domain.AgentThreadID, at time.Time) (leaseAcquisition, error) {
	if !grant.HasWriteAccess() {
		return leaseAcquisition{}, nil
	}
	active, err := s.leases.GetActiveByWorkspace(ctx, grant.WorkspaceKey)
	if err == nil {
		if active.OwnerThreadID == threadID {
			return leaseAcquisition{}, domain.ErrLeaseConflict
		}
		return leaseAcquisition{}, domain.ErrLeaseConflict
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return leaseAcquisition{}, err
	}
	lease, event, err := domain.AcquireWorkspaceWriteLease(domain.NewWorkspaceLeaseID(), grant.WorkspaceKey, threadID, grant, at)
	if err != nil {
		return leaseAcquisition{}, err
	}
	if err := s.leases.Save(ctx, lease); err != nil {
		return leaseAcquisition{}, err
	}
	return leaseAcquisition{lease: lease, event: event}, nil
}

func (s *QueueScheduler) releaseLease(ctx context.Context, grant domain.CapabilityGrant, threadID domain.AgentThreadID, at time.Time) (domain.DomainEvent, error) {
	if !grant.HasWriteAccess() {
		return domain.DomainEvent{}, nil
	}
	lease, err := s.leases.GetActiveByWorkspace(ctx, grant.WorkspaceKey)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.DomainEvent{}, nil
	}
	if err != nil {
		return domain.DomainEvent{}, err
	}
	if lease.OwnerThreadID != threadID {
		return domain.DomainEvent{}, domain.ErrLeaseConflict
	}
	event, err := lease.Release(at)
	if err != nil {
		return domain.DomainEvent{}, err
	}
	if err := s.leases.Release(ctx, lease); err != nil {
		return domain.DomainEvent{}, err
	}
	return event, nil
}

func (s *QueueScheduler) registerRuntime(threadID domain.AgentThreadID, runtime runtime.Runtime) uint64 {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	s.nextGeneration++
	entry := runtimeEntry{generation: s.nextGeneration, runtime: runtime}
	if previous, exists := s.current[threadID]; exists {
		s.retired = append(s.retired, previous.runtime)
	}
	s.current[threadID] = entry
	return entry.generation
}

func (s *QueueScheduler) unregisterRuntime(threadID domain.AgentThreadID, generation uint64) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	entry, exists := s.current[threadID]
	if exists && entry.generation == generation {
		delete(s.current, threadID)
	}
}

func (s *QueueScheduler) threadGuard(threadID domain.AgentThreadID) *sync.Mutex {
	s.guardMu.Lock()
	defer s.guardMu.Unlock()
	guard := s.guards[threadID]
	if guard == nil {
		guard = &sync.Mutex{}
		s.guards[threadID] = guard
	}
	return guard
}

func (s *QueueScheduler) isShuttingDown() bool {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	return s.shuttingDown
}

func validateQueueRequest(request QueueTaskRequest, thread domain.AgentThread) error {
	if request.TaskSessionID == "" || request.AgentThreadID == "" || request.TaskSessionID != thread.TaskSessionID || request.AgentThreadID != thread.ID {
		return invalidSchedulerState("queue task does not belong to the agent thread")
	}
	if err := request.TaskPacket.Validate(); err != nil {
		return err
	}
	if err := request.ContextManifest.Validate(); err != nil {
		return err
	}
	if err := request.Grant.Validate(); err != nil {
		return err
	}
	if request.Grant.ContextManifestRef != request.ContextManifest.ID {
		return invalidSchedulerState("grant and context manifest do not match")
	}
	if err := request.Execution.Validate(); err != nil {
		return err
	}
	return nil
}

func validateMaterializedItem(thread domain.AgentThread, item domain.QueuedWorkItem, packet domain.TaskPacket, manifest domain.ContextManifest, grant domain.CapabilityGrant) error {
	if item.TaskSessionID != thread.TaskSessionID || item.AgentThreadID != thread.ID {
		return invalidSchedulerState("queued work item does not belong to the agent thread")
	}
	if packet.ID != item.TaskPacketID || manifest.ID != item.ContextManifestID || grant.ID != item.CapabilityGrantID {
		return invalidSchedulerState("queued work item references changed execution inputs")
	}
	if grant.ContextManifestRef != manifest.ID {
		return invalidSchedulerState("queued grant and manifest do not match")
	}
	if err := packet.Validate(); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	return grant.Validate()
}

func invalidSchedulerState(message string) error {
	return fmt.Errorf("queue scheduler state: %s", message)
}

func (s *QueueScheduler) appendEvent(ctx context.Context, events ...domain.DomainEvent) error {
	if s.events == nil {
		return nil
	}
	for _, event := range events {
		if event.ID == "" {
			continue
		}
		if err := s.events.Append(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func queuedEvent(item domain.QueuedWorkItem, at time.Time) domain.DomainEvent {
	return domain.DomainEvent{
		ID: domain.NewEventID(), Type: domain.EventWorkItemQueued, OccurredAt: at.UTC(),
		TaskSession: item.TaskSessionID, AgentThread: item.AgentThreadID, WorkItem: item.ID,
		Payload: map[string]string{"status": string(item.Status), "sequence": fmt.Sprintf("%d", item.Sequence)},
	}
}

func startedEvent(item domain.QueuedWorkItem, at time.Time) domain.DomainEvent {
	return domain.DomainEvent{
		ID: domain.NewEventID(), Type: domain.EventWorkItemStarted, OccurredAt: at.UTC(),
		TaskSession: item.TaskSessionID, AgentThread: item.AgentThreadID, AgentRun: item.AgentRunID, WorkItem: item.ID,
		Payload: map[string]string{"status": string(domain.WorkItemRunning), "sequence": fmt.Sprintf("%d", item.Sequence)},
	}
}
