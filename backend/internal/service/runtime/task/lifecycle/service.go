// Package lifecycle 负责 Task 开始确认和结束状态的持久化。
package lifecycle

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/task"

	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"praxis/internal/logging"
	"praxis/internal/repository"
	"praxis/internal/system"
)

const (
	TerminalTaskEnded       = "task_ended"
	TerminalRequestCanceled = "request_canceled"
)

// TerminalEvent 是事务提交后发布的最小终态事实。
type TerminalEvent struct {
	Kind           string
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	TaskID         contracts.TaskID
	Outcome        string
	FailureCode    contracts.TaskFailureCode
	FailureMessage string
}

type Config struct {
	Transactions  repository.TxRunner
	Sessions      repository.SessionRepository
	Agents        repository.SessionAgentRepository
	Tasks         repository.TaskRepository
	Controls      repository.AgentControlCommandRepository
	Messages      repository.MessageLoader
	Clock         system.Clock
	Logger        *logging.Logger
	EventObserver func(TerminalEvent)
}

type Service struct {
	tx            repository.TxRunner
	sessions      repository.SessionRepository
	agents        repository.SessionAgentRepository
	tasks         repository.TaskRepository
	controls      repository.AgentControlCommandRepository
	messages      repository.MessageLoader
	clock         system.Clock
	logger        *logging.Logger
	eventObserver func(TerminalEvent)
}

type Params struct {
	TaskID         contracts.TaskID
	Outcome        taskmodel.TaskOutcome
	FailureCode    contracts.TaskFailureCode
	FailureMessage string
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"sessions":     config.Sessions,
		"agents":       config.Agents,
		"tasks":        config.Tasks,
		"controls":     config.Controls,
		"messages":     config.Messages,
	} {
		if value == nil {
			return nil, fmt.Errorf("task lifecycle service %s is required", name)
		}
	}
	return &Service{
		tx:            config.Transactions,
		sessions:      config.Sessions,
		agents:        config.Agents,
		tasks:         config.Tasks,
		controls:      config.Controls,
		messages:      config.Messages,
		clock:         system.ClockOrDefault(config.Clock),
		logger:        logging.NewFactory().Ensure(config.Logger),
		eventObserver: config.EventObserver,
	}, nil
}

// Start 将指定执行推进到 Running；重复确认不会改变开始时间。
// 已结束的执行不能重新启动，用户消息正文保留用于重复请求校验。
// taskID 来自 runtime 准入时已校验的持久化 Task。
func (s *Service) Start(ctx context.Context, taskID contracts.TaskID) error {
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
			return contracts.New(contracts.AgentUnavailable, "")
		}
		if err := task.MarkRunning(s.clock.Now()); err != nil {
			return err
		}
		return s.tasks.Save(txCtx, task)
	})
}

// End 接收 runtime 的执行结果，原子更新 Task、Agent 和控制命令。
// 返回控制命令仲裁后的持久化回合；重复结束返回已有终态，不再次发布事件。
func (s *Service) End(ctx context.Context, taskID contracts.TaskID, outcome taskmodel.TaskOutcome, failureCode contracts.TaskFailureCode, failureMessage string) (taskmodel.Task, error) {
	params := Params{
		TaskID:         taskID,
		Outcome:        outcome,
		FailureCode:    contracts.TaskFailureCode(strings.TrimSpace(string(failureCode))),
		FailureMessage: strings.TrimSpace(failureMessage),
	}
	if ctx == nil {
		return taskmodel.Task{}, errors.New("task end context is required")
	}
	if !knownOutcome(params.Outcome) {
		return taskmodel.Task{}, contracts.New(contracts.InvalidRequest, "")
	}
	var endedTask taskmodel.Task
	var firstEnd bool
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
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
			if params.FailureCode != contracts.TaskFailureRequestCanceled {
				params.Outcome = taskmodel.TaskPaused
			}
			if control.Kind == agentmodel.AgentControlCancel {
				params.Outcome = taskmodel.TaskInterrupted
			}
			params.FailureCode = contracts.TaskFailureRequestCanceled
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
			return contracts.New(contracts.AgentUnavailable, "")
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
	// 每轮仅发布一个终态事件；取消原因来自持久化控制命令，不由 outcome 推断。
	if s.eventObserver != nil {
		kind := TerminalTaskEnded
		if endedTask.FailureCode == contracts.TaskFailureRequestCanceled {
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

func knownOutcome(outcome taskmodel.TaskOutcome) bool {
	switch outcome {
	case taskmodel.TaskCompleted, taskmodel.TaskYielded, taskmodel.TaskPaused, taskmodel.TaskFailed, taskmodel.TaskInterrupted:
		return true
	default:
		return false
	}
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
