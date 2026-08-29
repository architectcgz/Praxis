package orchestrate

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
	coresession "praxis/internal/core/session"
)

type AgentSessionResolver func(domain.SessionID, domain.AgentID) (coresession.TranscriptReceiptStore, error)

type RuntimeSnapshotResolver func(context.Context, domain.Agent) (domain.RuntimeExecutionSnapshot, error)

type RecoveryCoordinatorConfig struct {
	Agents       persistence.AgentRepository
	Executions   persistence.AgentExecutionRepository
	Controls     persistence.AgentControlRequestRepository
	Deliveries   persistence.ContextDeliveryRepository
	Orchestrator *AgentOrchestrator
	Scheduler    *ExecutionScheduler
	Delivery     *DeliveryCoordinator
	Sessions     AgentSessionResolver
	Snapshot     RuntimeSnapshotResolver
}

// RecoveryCoordinator rebuilds progress from SQLite and Agent JSONL receipts.
// It never assumes an interrupted model or tool operation was safe to replay.
type RecoveryCoordinator struct {
	agents       persistence.AgentRepository
	executions   persistence.AgentExecutionRepository
	controls     persistence.AgentControlRequestRepository
	deliveries   persistence.ContextDeliveryRepository
	orchestrator *AgentOrchestrator
	scheduler    *ExecutionScheduler
	delivery     *DeliveryCoordinator
	sessions     AgentSessionResolver
	snapshot     RuntimeSnapshotResolver
}

func NewRecoveryCoordinator(config RecoveryCoordinatorConfig) (*RecoveryCoordinator, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "agents", value: config.Agents},
		{name: "executions", value: config.Executions},
		{name: "deliveries", value: config.Deliveries},
		{name: "orchestrator", value: config.Orchestrator},
		{name: "scheduler", value: config.Scheduler},
		{name: "delivery", value: config.Delivery},
		{name: "sessions", value: config.Sessions},
		{name: "snapshot", value: config.Snapshot},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("recovery coordinator %s is required", required.name)
		}
	}
	return &RecoveryCoordinator{
		agents:       config.Agents,
		executions:   config.Executions,
		controls:     config.Controls,
		deliveries:   config.Deliveries,
		orchestrator: config.Orchestrator,
		scheduler:    config.Scheduler,
		delivery:     config.Delivery,
		sessions:     config.Sessions,
		snapshot:     config.Snapshot,
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
	agents, err := c.agents.ListAll(ctx, 1000)
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

	executions, err := c.executions.ListRecoverable(ctx, 1000)
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
		if execution.Status == domain.ExecutionStarting && start == nil {
			continue
		}
		if err := c.orchestrator.SettleExecution(ctx, ExecutionSettlement{
			ExecutionID: execution.ID,
			Outcome:     domain.ExecutionInterrupted,
			FailureCode: domain.ExecutionFailureRecoveryInterrupted,
		}); err != nil {
			return report, fmt.Errorf("interrupt recovered execution %s: %w", execution.ID, err)
		}
		report.InterruptedExecutions++
	}

	deliveries, err := c.deliveries.ListInFlight(ctx, 1000)
	if err != nil {
		return report, err
	}
	for _, delivery := range deliveries {
		agent, err := c.agents.Get(ctx, delivery.TargetAgentID)
		if err != nil {
			return report, fmt.Errorf("load delivery target agent %s: %w", delivery.ID, err)
		}
		snapshot, err := c.snapshot(ctx, agent)
		if err != nil {
			return report, fmt.Errorf("resolve delivery snapshot %s: %w", delivery.ID, err)
		}
		attempt, err := c.delivery.TryDeliver(context.WithoutCancel(ctx), delivery.ID, snapshot)
		if err != nil {
			return report, fmt.Errorf("recover context delivery %s: %w", delivery.ID, err)
		}
		if attempt.Completed {
			report.RecoveredDeliveries++
		}
	}

	for _, agent := range agents {
		if c.controls != nil {
			controls, err := c.controls.ListOpenByAgent(ctx, agent.ID, 100)
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

	starting, err := c.executions.ListStarting(ctx, 1000)
	if err != nil {
		return report, err
	}
	if err := c.scheduler.ActivateStarting(context.WithoutCancel(ctx), len(starting), c.orchestrator); err != nil {
		return report, err
	}
	report.ActivatedStarting = len(starting)
	c.orchestrator.SetReady(true)
	return report, nil
}
