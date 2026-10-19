package toolexecution

import (
	"context"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/security"
	taskmodel "praxis/internal/service/agent"
	"time"
)

const riskReviewTimeout = time.Minute

// review 仅编排审核身份、外部模型调用和结果提交；所有状态决策由 Core 完成。
func (s *Executor) review(ctx context.Context, id identity.ToolExecutionID) (executionmodel.Execution, error) {
	if _, loaded := s.reviewInFlight.LoadOrStore(id, struct{}{}); loaded {
		return executionmodel.Execution{}, fault.New(taskmodel.ExecutionBusy, "this operation is already being reviewed")
	}
	defer s.reviewInFlight.Delete(id)
	var invocation executionmodel.Execution
	var task taskmodel.Execution
	var attempt executionmodel.RiskReview
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		var err error
		invocation, err = s.executions.Get(txCtx, id)
		if err != nil || !invocation.NeedsRiskReview() {
			return err
		}
		task, err = s.tasks.Get(txCtx, invocation.ExecutionID)
		if err != nil {
			return err
		}
		if !task.Active() {
			return fault.ErrRequestConflict
		}
		decision, err := s.rules.Decide(txCtx, invocation)
		if err != nil {
			return err
		}
		if invocation.Status == executionmodel.StatusReviewing && invocation.Decision != decision {
			if err := invocation.ResetReview(decision, s.clock.Now()); err != nil {
				return err
			}
		} else if invocation.Status == executionmodel.StatusPrepared {
			if err := invocation.ApplyAuthorization(decision, s.clock.Now()); err != nil {
				return err
			}
		}
		if !invocation.NeedsRiskReview() {
			return s.executions.Save(txCtx, invocation)
		}
		sequence := max(uint64(1), task.Progress.NextSequence)
		task.Progress.NextSequence = sequence + 1
		task.Progress.ModelRequests++
		attempt = executionmodel.RiskReview{
			TurnID:          identity.NewTurnID(task.ID, sequence),
			ArgumentsDigest: executionmodel.ArgumentsDigest(invocation.Name, invocation.NormalizedArguments),
			ProviderID:      task.Config.Model.ProviderID,
			ModelID:         task.Config.Model.ModelID,
			StartedAt:       s.clock.Now().UTC(),
		}
		if err := invocation.BeginReview(attempt); err != nil {
			return err
		}
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		return s.executions.Save(txCtx, invocation)
	})
	if err != nil || attempt.TurnID == "" {
		return invocation, err
	}

	reviewCtx, cancel := context.WithTimeout(ctx, riskReviewTimeout)
	assessment, reviewErr := s.reviewOperation(reviewCtx, task, invocation, attempt)
	if reviewErr == nil {
		reviewErr = reviewCtx.Err()
	}
	cancel()
	if err := ctx.Err(); err != nil {
		return executionmodel.Execution{}, err
	}
	if reviewErr != nil {
		assessment = security.RiskAssessment{Verdict: security.RiskUncertain, Reason: "Risk assessment failed or returned an invalid result; manual approval is required."}
	}
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.executions.Get(txCtx, id)
		if err != nil {
			return err
		}
		currentTask, err := s.tasks.Get(txCtx, current.ExecutionID)
		if err != nil {
			return err
		}
		if !currentTask.Active() {
			return fault.ErrRequestConflict
		}
		decision, err := s.rules.Decide(txCtx, current)
		if err != nil {
			return err
		}
		if err := current.ApplyRiskAssessment(attempt.TurnID, assessment, decision, s.clock.Now()); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, current); err != nil {
			return err
		}
		invocation = current
		return nil
	})
	return invocation, err
}
