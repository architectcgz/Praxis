package orchestrate

import (
	"context"
	"errors"
	"fmt"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainworkflow "praxis/internal/core/domain/workflow"
	"time"

	domainagent "praxis/internal/core/domain/agent"
	"praxis/internal/core/persistence"
	coresession "praxis/internal/core/session"
)

type AgentSessionResolver func(domainfoundation.SessionID, domainfoundation.AgentID) (coresession.TranscriptReceiptStore, error)

type RecoveryCoordinatorConfig struct {
	Agents       persistence.AgentRepository
	Contexts     persistence.SessionContextRepository
	Executions   persistence.AgentExecutionRepository
	Waits        persistence.WaitConditionRepository
	Controls     persistence.AgentControlRequestRepository
	Deliveries   persistence.ContextDeliveryRepository
	Orchestrator *AgentOrchestrator
	Scheduler    *ExecutionScheduler
	Delivery     *DeliveryCoordinator
	Sessions     AgentSessionResolver
}

// RecoveryCoordinator rebuilds progress from SQLite and Agent JSONL receipts.
// It never assumes an interrupted model or tool operation was safe to replay.
type RecoveryCoordinator struct {
	agents       persistence.AgentRepository
	contexts     persistence.SessionContextRepository
	executions   persistence.AgentExecutionRepository
	waits        persistence.WaitConditionRepository
	controls     persistence.AgentControlRequestRepository
	deliveries   persistence.ContextDeliveryRepository
	orchestrator *AgentOrchestrator
	scheduler    *ExecutionScheduler
	delivery     *DeliveryCoordinator
	sessions     AgentSessionResolver
}

func NewRecoveryCoordinator(config RecoveryCoordinatorConfig) (*RecoveryCoordinator, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "agents", value: config.Agents},
		{name: "session contexts", value: config.Contexts},
		{name: "executions", value: config.Executions},
		{name: "waits", value: config.Waits},
		{name: "controls", value: config.Controls},
		{name: "deliveries", value: config.Deliveries},
		{name: "orchestrator", value: config.Orchestrator},
		{name: "scheduler", value: config.Scheduler},
		{name: "delivery", value: config.Delivery},
		{name: "sessions", value: config.Sessions},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("recovery coordinator %s is required", required.name)
		}
	}
	return &RecoveryCoordinator{
		agents:       config.Agents,
		contexts:     config.Contexts,
		executions:   config.Executions,
		waits:        config.Waits,
		controls:     config.Controls,
		deliveries:   config.Deliveries,
		orchestrator: config.Orchestrator,
		scheduler:    config.Scheduler,
		delivery:     config.Delivery,
		sessions:     config.Sessions,
	}, nil
}

type RecoveryReport struct {
	RepairedSessions      int
	ActivatedStarting     int
	RecoveredSettlement   int
	InterruptedExecutions int
	AppliedControls       int
	RecoveredDeliveries   int
	StartedQueuedWork     int
}

// Recover keeps command admission closed until JSONL repair, receipt
// reconciliation, and durable execution convergence have all completed.
func (c *RecoveryCoordinator) Recover(ctx context.Context) (RecoveryReport, error) {
	if ctx == nil {
		return RecoveryReport{}, errors.New("recovery context is required")
	}
	c.orchestrator.SetReady(false)
	report := RecoveryReport{}
	agents, err := listRecoveryAgents(ctx, c.agents)
	if err != nil {
		return report, err
	}
	for _, agent := range agents {
		store, err := c.sessions(agent.SessionID, agent.ID)
		if err != nil {
			return report, fmt.Errorf("resolve agent session %s: %w", agent.ID, err)
		}
		if store == nil {
			return report, fmt.Errorf("resolve agent session %s: returned nil store", agent.ID)
		}
		repaired, err := store.Repair(ctx)
		if err != nil {
			return report, fmt.Errorf("repair agent session %s: %w", agent.ID, err)
		}
		if repaired {
			report.RepairedSessions++
		}
	}
	if err := verifySessionContexts(ctx, c.contexts, agents); err != nil {
		return report, err
	}
	if err := scanPendingWaits(ctx, c.waits, agents); err != nil {
		return report, err
	}

	executions, err := listRecoverableExecutions(ctx, c.executions)
	if err != nil {
		return report, err
	}
	for _, execution := range executions {
		store, err := c.sessions(execution.SessionID, execution.AgentID)
		if err != nil {
			return report, fmt.Errorf("resolve execution session %s: %w", execution.ID, err)
		}
		settlement, err := store.FindExecutionSettlement(ctx, execution.ID)
		if err != nil {
			return report, fmt.Errorf("find settlement receipt %s: %w", execution.ID, err)
		}
		if settlement != nil {
			if err := c.orchestrator.SettleExecution(ctx, ExecutionSettlement{
				ExecutionID: execution.ID,
				Outcome:     settlement.Outcome,
				FailureCode: settlement.FailureCode,
			}); err != nil {
				return report, fmt.Errorf("recover settled execution %s: %w", execution.ID, err)
			}
			report.RecoveredSettlement++
			continue
		}
		start, err := store.FindExecutionStart(ctx, execution.ID)
		if err != nil {
			return report, fmt.Errorf("find start receipt %s: %w", execution.ID, err)
		}
		if execution.Status == domainexecution.ExecutionStarting && start == nil {
			continue
		}
		if err := c.orchestrator.SettleExecution(ctx, ExecutionSettlement{
			ExecutionID: execution.ID,
			Outcome:     domainexecution.ExecutionInterrupted,
			FailureCode: domainexecution.ExecutionFailureRecoveryInterrupted,
		}); err != nil {
			return report, fmt.Errorf("interrupt recovered execution %s: %w", execution.ID, err)
		}
		report.InterruptedExecutions++
	}

	deliveries, err := listRecoverableDeliveries(ctx, c.deliveries)
	if err != nil {
		return report, err
	}
	for _, delivery := range deliveries {
		attempt, err := c.delivery.TryDeliver(context.WithoutCancel(ctx), delivery.ID)
		if err != nil {
			return report, fmt.Errorf("recover context delivery %s: %w", delivery.ID, err)
		}
		if attempt.Completed {
			report.RecoveredDeliveries++
		}
	}

	for _, agent := range agents {
		controls, err := listRecoverableControls(ctx, c.controls, agent.ID)
		if err != nil {
			return report, fmt.Errorf("list control requests for agent %s: %w", agent.ID, err)
		}
		for _, control := range controls {
			if err := c.orchestrator.ApplyControlRequest(ctx, control.ID); err != nil {
				if hasCommandErrorCode(err, CommandErrorAgentUnavailable) {
					continue
				}
				return report, fmt.Errorf("apply control request %s: %w", control.ID, err)
			}
			report.AppliedControls++
		}
	}

	for _, agent := range agents {
		started, err := c.orchestrator.StartNextQueuedWork(context.WithoutCancel(ctx), agent.ID)
		if err != nil {
			return report, fmt.Errorf("start recovered queued work for agent %s: %w", agent.ID, err)
		}
		if started.Started {
			report.StartedQueuedWork++
		}
	}

	starting, err := listStartingExecutions(ctx, c.executions)
	if err != nil {
		return report, err
	}
	if err := c.scheduler.ActivateStarting(context.WithoutCancel(ctx), starting, c.orchestrator); err != nil {
		return report, err
	}
	report.ActivatedStarting = len(starting)
	c.orchestrator.SetReady(true)
	return report, nil
}

func scanPendingWaits(ctx context.Context, repository persistence.WaitConditionRepository, agents []domainagent.Agent) error {
	paged, ok := repository.(persistence.WaitConditionRecoveryRepository)
	if !ok {
		return errors.New("wait condition recovery cursor is unavailable")
	}
	for _, agent := range agents {
		var after domainfoundation.WaitConditionID
		for {
			page, err := paged.ListUnresolvedByAgentAfter(ctx, agent.ID, after, 512)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				break
			}
			after = page[len(page)-1].ID
			if len(page) < 512 {
				break
			}
		}
	}
	return nil
}

func verifySessionContexts(ctx context.Context, repository persistence.SessionContextRepository, agents []domainagent.Agent) error {
	checked := make(map[domainfoundation.SessionID]struct{})
	for _, agent := range agents {
		if _, ok := checked[agent.SessionID]; ok {
			continue
		}
		checked[agent.SessionID] = struct{}{}
		current, err := repository.CurrentRevision(ctx, agent.SessionID)
		if err != nil {
			return err
		}
		if current == 0 {
			return fmt.Errorf("session %s has no context revision", agent.SessionID)
		}
		var seen uint64
		for seen < current {
			page, err := repository.List(ctx, agent.SessionID, seen, 512)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				return fmt.Errorf("session %s context revision sequence is incomplete", agent.SessionID)
			}
			for _, entry := range page {
				seen++
				if entry.Revision != seen {
					return fmt.Errorf("session %s context revision sequence is incomplete", agent.SessionID)
				}
			}
		}
	}
	return nil
}

func listRecoverableControls(ctx context.Context, repository persistence.AgentControlRequestRepository, agentID domainfoundation.AgentID) ([]domainworkflow.AgentControlRequest, error) {
	paged, ok := repository.(persistence.AgentControlRecoveryRepository)
	if !ok {
		return nil, errors.New("agent control recovery cursor is unavailable")
	}
	const pageSize = 512
	result := make([]domainworkflow.AgentControlRequest, 0)
	var after domainfoundation.AgentControlRequestID
	for {
		page, err := paged.ListOpenByAgentAfter(ctx, agentID, after, pageSize)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < pageSize {
			return result, nil
		}
		after = page[len(page)-1].ID
	}
}

func listRecoveryAgents(ctx context.Context, repository persistence.AgentRepository) ([]domainagent.Agent, error) {
	if paged, ok := repository.(persistence.AgentRecoveryRepository); ok {
		result := make([]domainagent.Agent, 0)
		var sessionID domainfoundation.SessionID
		var agentID domainfoundation.AgentID
		for {
			page, err := paged.ListAllAfter(ctx, sessionID, agentID, 512)
			if err != nil {
				return nil, err
			}
			result = append(result, page...)
			if len(page) < 512 {
				return result, nil
			}
			last := page[len(page)-1]
			sessionID, agentID = last.SessionID, last.ID
		}
	}
	return nil, errors.New("agent recovery cursor is unavailable")
}

func listRecoverableExecutions(ctx context.Context, repository persistence.AgentExecutionRepository) ([]domainexecution.AgentExecution, error) {
	paged, ok := repository.(persistence.ExecutionRecoveryRepository)
	if !ok {
		return nil, errors.New("execution recovery cursor is unavailable")
	}
	result := make([]domainexecution.AgentExecution, 0)
	var after time.Time
	var afterID domainfoundation.AgentExecutionID
	for {
		page, err := paged.ListRecoverableAfter(ctx, after, afterID, 512)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < 512 {
			return result, nil
		}
		last := page[len(page)-1]
		after, afterID = last.CreatedAt, last.ID
	}
}

func listStartingExecutions(ctx context.Context, repository persistence.AgentExecutionRepository) ([]domainexecution.AgentExecution, error) {
	paged, ok := repository.(persistence.ExecutionRecoveryRepository)
	if !ok {
		return nil, errors.New("starting execution recovery cursor is unavailable")
	}
	result := make([]domainexecution.AgentExecution, 0)
	var after time.Time
	var afterID domainfoundation.AgentExecutionID
	for {
		page, err := paged.ListStartingAfter(ctx, after, afterID, 512)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < 512 {
			return result, nil
		}
		last := page[len(page)-1]
		after, afterID = last.CreatedAt, last.ID
	}
}

func listRecoverableDeliveries(ctx context.Context, repository persistence.ContextDeliveryRepository) ([]domainworkflow.ContextDelivery, error) {
	paged, ok := repository.(persistence.ContextDeliveryRecoveryRepository)
	if !ok {
		return nil, errors.New("context delivery recovery cursor is unavailable")
	}
	const pageSize = 512
	result := make([]domainworkflow.ContextDelivery, 0)
	var after domainfoundation.DeliveryID
	for {
		page, err := paged.ListInFlightAfter(ctx, after, pageSize)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < pageSize {
			return result, nil
		}
		after = page[len(page)-1].ID
	}
}
