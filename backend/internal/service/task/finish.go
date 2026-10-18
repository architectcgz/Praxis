package task

import (
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/task"

	"context"
	"errors"
	"time"

	"praxis/internal/core/fault"
	"praxis/internal/core/identity"
	"praxis/internal/hooks"
)

const (
	TerminalTaskEnded       = "task_ended"
	TerminalRequestCanceled = "request_canceled"
)

// TerminalEvent 是事务提交后发布的最小终态事实。
type TerminalEvent struct {
	Kind           string
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	TaskID         identity.TaskID
	Outcome        string
	FailureCode    taskmodel.TaskFailureCode
	FailureMessage string
}

type settleParams struct {
	TaskID         identity.TaskID
	Outcome        taskmodel.TaskOutcome
	FailureCode    taskmodel.TaskFailureCode
	FailureMessage string
}

// ConfirmRunning 将指定执行推进到 Running；重复确认不会改变开始时间。
// 已结束的执行不能重新启动，用户消息正文保留用于重复请求校验。
// taskID 来自 runtime 准入时已校验的持久化 Task。
func (s *Service) ConfirmRunning(ctx context.Context, taskID identity.TaskID) error {
	if ctx == nil {
		return errors.New("task start context is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		task, err := s.tasks.Get(txCtx, taskID)
		if err != nil {
			return err
		}
		if task.Status == taskmodel.TaskRunning || task.Status == taskmodel.TaskEnding {
			return nil
		}
		if task.Status != taskmodel.TaskStarting {
			return fault.New(agentmodel.UnavailableCode, "")
		}
		if err := task.MarkRunning(s.clock.Now()); err != nil {
			return err
		}
		return s.tasks.Save(txCtx, task)
	})
}

// Settle 接收 runtime 的执行结果，原子更新 Task、Agent 和控制命令。
// 返回控制命令仲裁后的持久化回合；重复结束返回已有终态，不再次发布事件。
// OperationHook 在事务外观察结算前后；回滚时只返回 Err，不把待提交 Outcome 当作最终结果。
func (s *Service) Settle(ctx context.Context, taskID identity.TaskID, outcome taskmodel.TaskOutcome, failureCode taskmodel.TaskFailureCode, failureMessage string) (result taskmodel.Task, err error) {
	params := settleParams{
		TaskID:         taskID,
		Outcome:        outcome,
		FailureCode:    failureCode,
		FailureMessage: failureMessage,
	}
	if ctx == nil {
		return taskmodel.Task{}, errors.New("task end context is required")
	}
	if !taskmodel.ValidTaskOutcome(params.Outcome) {
		return taskmodel.Task{}, fault.New(fault.InvalidRequest, "")
	}
	// 只预读不可变归属，Hook 不能在持有仓储事务锁时启动；状态与控制命令仍在事务内重新仲裁。
	observedTask, err := s.tasks.Get(ctx, params.TaskID)
	if err != nil {
		s.logger.Errorf("operation=task_end task_id=%s status=failed outcome=%s error=%v", params.TaskID, params.Outcome, err)
		return taskmodel.Task{}, err
	}
	ctx, scope := s.hookDispatcher.Start(ctx, hooks.Operation{
		Identity: hooks.Identity{
			SessionID: observedTask.SessionID,
			AgentID:   observedTask.AgentID,
			TaskID:    observedTask.ID,
		},
		Kind: hooks.OperationTaskSettle,
		Name: "settle_task",
	})
	defer func() { scope.End(hooks.Result{Err: err, Outcome: result.Outcome}) }()
	var endedTask taskmodel.Task
	var firstEnd bool
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		task, err := s.tasks.Get(txCtx, params.TaskID)
		if err != nil {
			return err
		}
		endedTask = task
		if task.Status == taskmodel.TaskEnded {
			return nil
		}
		controls, err := s.controls.ListOpenByAgent(txCtx, task.AgentID, 100)
		if err != nil {
			return err
		}
		// 结束事务之前提交的控制命令优先，取消与正常完成竞争时以提交顺序为准。
		for _, control := range controls {
			if control.TargetTaskID != task.ID {
				continue
			}
			if params.FailureCode != taskmodel.TaskFailureRequestCanceled {
				params.Outcome = taskmodel.TaskPaused
			}
			if control.Kind == agentmodel.AgentControlCancel {
				params.Outcome = taskmodel.TaskInterrupted
			}
			params.FailureCode = taskmodel.TaskFailureRequestCanceled
			params.FailureMessage = ""
		}
		at := s.clock.Now()
		if task.Status == taskmodel.TaskStarting {
			if err := task.MarkRunning(at); err != nil {
				return err
			}
		}
		if task.Status == taskmodel.TaskRunning {
			if err := task.BeginEnding(at); err != nil {
				return err
			}
		}
		if err := task.End(params.Outcome, params.FailureCode, at); err != nil {
			return err
		}
		if params.Outcome == taskmodel.TaskFailed {
			task.FailureMessage = params.FailureMessage
		}
		agent, err := s.agents.Get(txCtx, task.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentTaskID != task.ID {
			return fault.New(agentmodel.UnavailableCode, "")
		}
		if err := agent.EndTask(params.Outcome, at); err != nil {
			return err
		}
		for index := range controls {
			control := &controls[index]
			if control.TargetTaskID != task.ID {
				continue
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
			if err := s.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		endedTask = task
		firstEnd = true
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		if err := s.reconcileToolResults(txCtx, task.SessionID, task.AgentID, false); err != nil {
			return err
		}
		return s.nameSessionAfterAnswer(txCtx, task, params.Outcome, at)
	})
	if err != nil {
		s.logger.Errorf(
			"operation=task_end request_id=%s session_id=%s agent_id=%s task_id=%s status=failed outcome=%s error=%v",
			endedTask.RequestID,
			endedTask.SessionID,
			endedTask.AgentID,
			params.TaskID,
			params.Outcome,
			err,
		)
		return taskmodel.Task{}, err
	}
	if !firstEnd {
		return endedTask, nil
	}
	s.logger.Infof(
		"operation=task_end request_id=%s session_id=%s agent_id=%s task_id=%s status=committed outcome=%s",
		endedTask.RequestID,
		endedTask.SessionID,
		endedTask.AgentID,
		params.TaskID,
		params.Outcome,
	)
	committedTask := endedTask
	committedTask.Config = endedTask.Config.Clone()
	committedTask.ContextSelection = endedTask.ContextSelection.Clone()
	if hookErr := s.hookDispatcher.OnCommitted(context.WithoutCancel(ctx), hooks.CommittedEvent{
		Identity: hooks.Identity{
			SessionID: endedTask.SessionID,
			AgentID:   endedTask.AgentID,
			TaskID:    endedTask.ID,
		},
		Task: &committedTask,
	}); hookErr != nil {
		s.logger.Errorf("task committed hook failed: %v", hookErr)
	}
	// 每轮仅发布一个终态事件；取消原因来自持久化控制命令，不由 outcome 推断。
	if s.eventObserver != nil {
		kind := TerminalTaskEnded
		if endedTask.FailureCode == taskmodel.TaskFailureRequestCanceled {
			kind = TerminalRequestCanceled
		}
		s.eventObserver(TerminalEvent{
			Kind:           kind,
			SessionID:      endedTask.SessionID,
			AgentID:        endedTask.AgentID,
			TaskID:         endedTask.ID,
			Outcome:        string(endedTask.Outcome),
			FailureCode:    endedTask.FailureCode,
			FailureMessage: endedTask.FailureMessage,
		})
	}
	return endedTask, nil
}

func (s *Service) nameSessionAfterAnswer(
	ctx context.Context,
	task taskmodel.Task,
	outcome taskmodel.TaskOutcome,
	at time.Time,
) error {
	if outcome != taskmodel.TaskCompleted && outcome != taskmodel.TaskYielded {
		return nil
	}
	stream, err := s.messages.LoadMessages(ctx, task.SessionID, task.AgentID, 0)
	if err != nil {
		return err
	}
	for _, message := range stream.Messages {
		if message.TaskID != task.ID.String() || message.Role != "user" {
			continue
		}
		session, err := s.sessions.Get(ctx, task.SessionID)
		if err != nil {
			return err
		}
		if session.Title == "" {
			session.NameFromInput(message.TextContent(), at)
			if err := s.sessions.Save(ctx, session); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}
