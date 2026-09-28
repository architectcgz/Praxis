// Package settlement 负责 Execution 回执确认和结算。
package settlement

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"
	workflowmodel "praxis/internal/workflow"

	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"praxis/internal/logging"
	"praxis/internal/repository"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

type QueueStarter interface {
	StartNextQueuedWork(context.Context, contracts.AgentID) (bool, error)
}

type Config struct {
	Transactions repository.TxRunner
	Sessions     repository.SessionRepository
	Agents       repository.SessionAgentRepository
	Executions   repository.AgentExecutionRepository
	QueuedWork   repository.QueuedWorkRepository
	Controls     repository.AgentControlCommandRepository
	Transcripts  runtimecontract.TranscriptLoader
	Clock        system.Clock
	Logger       *logging.Logger
}

type Service struct {
	tx          repository.TxRunner
	sessions    repository.SessionRepository
	agents      repository.SessionAgentRepository
	executions  repository.AgentExecutionRepository
	queuedWork  repository.QueuedWorkRepository
	controls    repository.AgentControlCommandRepository
	transcripts runtimecontract.TranscriptLoader
	clock       system.Clock
	logger      *logging.Logger
	queueMu     sync.RWMutex
	queue       QueueStarter
}

type Params struct {
	ExecutionID contracts.AgentExecutionID
	Outcome     executionmodel.ExecutionOutcome
	FailureCode contracts.ExecutionFailureCode
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"sessions":     config.Sessions,
		"agents":       config.Agents,
		"executions":   config.Executions,
		"queued work":  config.QueuedWork,
		"controls":     config.Controls,
		"transcripts":  config.Transcripts,
	} {
		if value == nil {
			return nil, fmt.Errorf("execution settlement service %s is required", name)
		}
	}
	return &Service{
		tx:          config.Transactions,
		sessions:    config.Sessions,
		agents:      config.Agents,
		executions:  config.Executions,
		queuedWork:  config.QueuedWork,
		controls:    config.Controls,
		transcripts: config.Transcripts,
		clock:       system.ClockOrDefault(config.Clock),
		logger:      logging.NewFactory().Ensure(config.Logger),
	}, nil
}

// SetQueueStarter attaches FIFO progression after queue construction finishes.
func (s *Service) SetQueueStarter(starter QueueStarter) error {
	if starter == nil {
		return errors.New("queue starter is required")
	}
	s.queueMu.Lock()
	s.queue = starter
	s.queueMu.Unlock()
	return nil
}

// ConfirmExecutionStart accepts a fsynced execution-start receipt before
// SQLite clears the temporary source content.
func (s *Service) ConfirmExecutionStart(ctx context.Context, receipt runtimecontract.ExecutionStartReceipt) error {
	if ctx == nil {
		return errors.New("execution start receipt context is required")
	}
	if strings.TrimSpace(receipt.ExecutionID.String()) == "" || strings.TrimSpace(receipt.EntryID) == "" {
		return contracts.New(contracts.InvalidRequest, "")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := s.executions.Get(txCtx, receipt.ExecutionID)
		if err != nil {
			return err
		}
		if execution.RequestID != receipt.RequestID {
			return contracts.New(contracts.InvalidRequest, "")
		}
		if execution.Status == executionmodel.ExecutionStarting {
			if err := execution.MarkRunning(s.clock.Now()); err != nil {
				return err
			}
		}
		if execution.Status != executionmodel.ExecutionRunning && execution.Status != executionmodel.ExecutionSettling {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		if execution.StartContent != "" {
			if err := execution.ClearStartContent(receipt.InputDigest); err != nil {
				return err
			}
		}
		return s.executions.Save(txCtx, execution)
	})
}

// SettleRuntimeExecution is the runtime callback after its JSONL settlement
// receipt is durable.
func (s *Service) SettleRuntimeExecution(ctx context.Context, executionID contracts.AgentExecutionID, outcome executionmodel.ExecutionOutcome, failureCode contracts.ExecutionFailureCode) error {
	return s.Settle(ctx, Params{ExecutionID: executionID, Outcome: outcome, FailureCode: failureCode})
}

// Settle applies execution, Agent, queued work and control transitions in one transaction.
func (s *Service) Settle(ctx context.Context, params Params) error {
	if ctx == nil {
		return errors.New("settlement context is required")
	}
	if strings.TrimSpace(params.ExecutionID.String()) == "" || !knownOutcome(params.Outcome) {
		return contracts.New(contracts.InvalidRequest, "")
	}
	var settledAgentID contracts.AgentID
	var settledExecution executionmodel.AgentExecution
	var advanceQueue bool
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := s.executions.Get(txCtx, params.ExecutionID)
		if err != nil {
			return err
		}
		settledExecution = execution
		if execution.Status == executionmodel.ExecutionSettled {
			return nil
		}
		at := s.clock.Now()
		if execution.Status == executionmodel.ExecutionStarting {
			if err := execution.MarkRunning(at); err != nil {
				return err
			}
		}
		if execution.Status == executionmodel.ExecutionRunning {
			if err := execution.BeginSettlement(at); err != nil {
				return err
			}
		}
		if err := execution.Settle(params.Outcome, params.FailureCode, at); err != nil {
			return err
		}
		agent, err := s.agents.Get(txCtx, execution.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentExecutionID != execution.ID {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		if err := agent.Settle(params.Outcome, at); err != nil {
			return err
		}
		if execution.WorkItemID != "" {
			work, err := s.queuedWork.Get(txCtx, execution.WorkItemID)
			if err != nil {
				return err
			}
			if work.AgentID != agent.ID || work.ExecutionID != execution.ID {
				return contracts.New(contracts.AgentUnavailable, "")
			}
			if err := work.Settle(execution.ID, params.Outcome, params.FailureCode, at); err != nil {
				return err
			}
			if err := s.queuedWork.Save(txCtx, work); err != nil {
				return err
			}
			advanceQueue = params.Outcome == executionmodel.ExecutionCompleted || params.Outcome == executionmodel.ExecutionYielded
		}
		controls, err := s.controls.ListOpenByAgent(txCtx, agent.ID, 100)
		if err != nil {
			return err
		}
		for index := range controls {
			control := &controls[index]
			if control.TargetExecutionID != execution.ID {
				continue
			}
			if control.Kind == workflowmodel.AgentControlClose {
				if err := agent.Close(at); err != nil {
					return err
				}
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
			if err := s.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		settledAgentID = agent.ID
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		return s.nameSessionAfterAnswer(txCtx, execution, params.Outcome, at)
	})
	if err != nil {
		s.logger.Errorf(
			"operation=execution_settle request_id=%s session_id=%s agent_id=%s execution_id=%s status=failed outcome=%s error=%v",
			settledExecution.RequestID,
			settledExecution.SessionID,
			settledExecution.AgentID,
			params.ExecutionID,
			params.Outcome,
			err,
		)
		return err
	}
	s.logger.Infof(
		"operation=execution_settle request_id=%s session_id=%s agent_id=%s execution_id=%s status=committed outcome=%s",
		settledExecution.RequestID,
		settledExecution.SessionID,
		settledExecution.AgentID,
		params.ExecutionID,
		params.Outcome,
	)
	if !advanceQueue {
		return nil
	}
	s.queueMu.RLock()
	queue := s.queue
	s.queueMu.RUnlock()
	if queue != nil {
		_, _ = queue.StartNextQueuedWork(context.WithoutCancel(ctx), settledAgentID)
	}
	return nil
}

func knownOutcome(outcome executionmodel.ExecutionOutcome) bool {
	switch outcome {
	case executionmodel.ExecutionCompleted, executionmodel.ExecutionYielded, executionmodel.ExecutionPaused, executionmodel.ExecutionFailed, executionmodel.ExecutionInterrupted:
		return true
	default:
		return false
	}
}

func (s *Service) nameSessionAfterAnswer(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	outcome executionmodel.ExecutionOutcome,
	at time.Time,
) error {
	if execution.Reason != executionmodel.ExecutionUserInput ||
		(outcome != executionmodel.ExecutionCompleted && outcome != executionmodel.ExecutionYielded) {
		return nil
	}
	transcript, err := s.transcripts.LoadTranscript(ctx, execution.SessionID, execution.AgentID)
	if err != nil {
		return err
	}
	for _, message := range transcript.Messages {
		if message.ExecutionID != execution.ID || message.Role != "user" {
			continue
		}
		session, err := s.sessions.Get(ctx, execution.SessionID)
		if err != nil {
			return err
		}
		if session.Title == "" {
			session.NameFromInput(message.Content, at)
			if err := s.sessions.Save(ctx, session); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}
