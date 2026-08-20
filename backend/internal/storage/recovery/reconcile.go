package recovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"praxis/internal/core/domain"
	"praxis/internal/storage/sessionlog"
	"praxis/internal/storage/sqlite"
)

// SessionResolver locates the per-thread JSONL store needed for repair and
// idempotent delivery reconciliation without exposing raw transcript paths.
type SessionResolver func(domain.TaskSessionID, domain.AgentThreadID) (*sessionlog.Store, error)

// Report records only durable state changes made during conservative recovery.
type Report struct {
	RepairedSessions     []sessionlog.RepairResult
	InterruptedRuns      []domain.AgentRunID
	ReleasedLeases       []domain.WorkspaceLeaseID
	RecoveredDeliveries  []domain.DeliveryID
	FailedDeliveries     []domain.DeliveryID
	InterruptedWorkItems []domain.WorkItemID
}

// Reconcile repairs session tails before changing SQLite state, then marks
// unknown in-flight work interrupted. It never auto-approves or replays effects.
func Reconcile(ctx context.Context, product *sqlite.Store, resolve SessionResolver, at time.Time) (Report, error) {
	if ctx == nil {
		return Report{}, errors.New("reconciliation context is required")
	}
	if product == nil {
		return Report{}, errors.New("sqlite product store is required")
	}
	if at.IsZero() {
		return Report{}, errors.New("reconciliation time is required")
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	report := Report{}
	if resolve != nil {
		refs, err := product.ListRecoveryThreads(ctx)
		if err != nil {
			return Report{}, err
		}
		for _, ref := range refs {
			store, err := resolve(ref.TaskSessionID, ref.AgentThreadID)
			if err != nil {
				return Report{}, fmt.Errorf("resolve session %s: %w", ref.AgentThreadID, err)
			}
			if store == nil {
				return Report{}, fmt.Errorf("resolve session %s: returned nil store", ref.AgentThreadID)
			}
			result, err := store.Repair(ctx)
			if err != nil {
				return Report{}, fmt.Errorf("repair session %s: %w", ref.AgentThreadID, err)
			}
			if result.Repaired {
				report.RepairedSessions = append(report.RepairedSessions, result)
			}
		}
	}

	repositories := product.Repositories()
	err := product.InTx(ctx, func(txCtx context.Context) error {
		runIDs, err := product.ListUnsettledRunIDs(txCtx)
		if err != nil {
			return err
		}
		interruptedThreads := make(map[domain.AgentThreadID]struct{})
		for _, runID := range runIDs {
			run, err := repositories.AgentRuns.Get(txCtx, runID)
			if err != nil {
				return err
			}
			if _, err := run.Settle(domain.RunInterrupted, at, "runtime_interrupted"); err != nil {
				return fmt.Errorf("settle interrupted run %s: %w", runID, err)
			}
			if err := repositories.AgentRuns.Save(txCtx, run); err != nil {
				return err
			}
			report.InterruptedRuns = append(report.InterruptedRuns, runID)
			interruptedThreads[run.AgentThreadID] = struct{}{}
		}
		for threadID := range interruptedThreads {
			thread, err := repositories.AgentThreads.Get(txCtx, threadID)
			if err != nil {
				return err
			}
			if thread.State != domain.ThreadRunning && thread.State != domain.ThreadPausing {
				continue
			}
			if _, err := thread.Settle(domain.RunInterrupted, at); err != nil {
				return fmt.Errorf("settle interrupted thread %s: %w", threadID, err)
			}
			if err := repositories.AgentThreads.Save(txCtx, thread); err != nil {
				return err
			}
		}
		leaseIDs, err := product.ListActiveLeaseIDs(txCtx)
		if err != nil {
			return err
		}
		for _, leaseID := range leaseIDs {
			lease, err := product.GetWorkspaceLease(txCtx, leaseID)
			if err != nil {
				return err
			}
			if _, owned := interruptedThreads[lease.OwnerThreadID]; !owned {
				continue
			}
			if _, err := lease.Release(at); err != nil {
				return err
			}
			if err := repositories.WorkspaceLeases.Release(txCtx, lease); err != nil {
				return err
			}
			report.ReleasedLeases = append(report.ReleasedLeases, leaseID)
		}
		items, err := product.ReconcileRunning(txCtx, at)
		if err != nil {
			return err
		}
		for _, item := range items {
			report.InterruptedWorkItems = append(report.InterruptedWorkItems, item.ID)
		}
		if resolve == nil {
			return nil
		}
		deliveryIDs, err := product.ListInFlightDeliveryIDs(txCtx)
		if err != nil {
			return err
		}
		for _, deliveryID := range deliveryIDs {
			delivery, err := repositories.Deliveries.Get(txCtx, deliveryID)
			if err != nil {
				return err
			}
			if delivery.Status == domain.DeliveryPending {
				continue
			}
			thread, err := repositories.AgentThreads.Get(txCtx, delivery.TargetThreadID)
			if err != nil {
				return err
			}
			store, err := resolve(thread.TaskSessionID, delivery.TargetThreadID)
			if err != nil {
				return fmt.Errorf("resolve delivery session %s: %w", delivery.TargetThreadID, err)
			}
			artifact, err := store.FindArtifact(txCtx, "", delivery.InjectionKey)
			if err != nil {
				return err
			}
			if artifact != nil {
				if _, err := delivery.MarkDelivered(artifact.EntryID, at); err != nil {
					return err
				}
				report.RecoveredDeliveries = append(report.RecoveredDeliveries, delivery.ID)
			} else {
				if _, err := delivery.Fail("delivery_interrupted", at); err != nil {
					return err
				}
				report.FailedDeliveries = append(report.FailedDeliveries, delivery.ID)
			}
			if err := repositories.Deliveries.Save(txCtx, delivery); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Report{}, fmt.Errorf("reconcile product state: %w", err)
	}
	return report, nil
}
