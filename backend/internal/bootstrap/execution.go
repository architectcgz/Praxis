package bootstrap

import (
	"context"
	"sync"

	taskservice "praxis/internal/agent/input"
	"praxis/internal/execution"
	"praxis/internal/execution/loop"
	"praxis/internal/hook"
	"praxis/internal/identity"
	"praxis/internal/security"
	"praxis/internal/session"
	"praxis/internal/storage/jsonl"
	queueservice "praxis/internal/task/service"
	"praxis/internal/toolcall"
)

func buildExecution(resources storageResources, repos jsonl.Repositories, config *configuration, sessions *session.Service, hooks *hook.Dispatcher, events *execution.EventPublisher, configMu *sync.RWMutex, runner *loop.Runner) (*execution.Runtime, error) {
	active := &execution.ActiveTasks{}
	tasks, err := taskservice.NewService(taskservice.Dependencies{
		Controls:       repos.Controls,
		Transactions:   resources.store,
		HookDispatcher: hooks,
		Logger:         resources.logger,
		EventObserver: func(value taskservice.TerminalEvent) {
			events.Publish(execution.Event{
				Kind:           value.Kind,
				SessionID:      value.SessionID,
				AgentID:        value.AgentID,
				ExecutionID:    value.ExecutionID,
				Outcome:        value.Outcome,
				FailureCode:    string(value.FailureCode),
				FailureMessage: value.FailureMessage,
			})
		},
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Agents:         repos.Agents,
		Tasks:          repos.Executions,
		Queue:          repos.Tasks,
		ToolExecutions: repos.ToolExecutions,
		Executions:     active,
		Messages:       repos.Messages,
		PrimaryAgent:   sessions,
		Definitions:    config.agents,
		Models:         config.models,
		Contexts:       repos.Contexts,
	})
	if err != nil {
		return nil, err
	}

	queue, err := queueservice.NewService(queueservice.Dependencies{
		Transactions:   resources.store,
		Tasks:          repos.Tasks,
		Agents:         repos.Agents,
		Messages:       repos.Messages,
		HookDispatcher: hooks,
		Logger:         resources.logger,
	})
	if err != nil {
		return nil, err
	}
	return execution.NewRuntime(execution.Dependencies{
		Transactions: resources.store,
		Tasks:        tasks,
		Queue:        queue,
		Progress:     tasks,
		Recovery: recoverySource{
			tasks:    repos.Executions,
			sessions: repos.Sessions,
			agents:   repos.Agents,
		},
		ActiveTasks:    active,
		ToolExecutions: repos.ToolExecutions,
		Messages:       repos.Messages,
		Writers:        repos.Messages,
		Models:         config.models,
		Tools:          config.tools,
		Rules: toolcall.ApprovalRules{
			Catalog: config.tools,
			DefaultMode: func(ctx context.Context, sessionID identity.SessionID) (security.ApprovalMode, error) {
				session, err := repos.Sessions.Get(ctx, sessionID)
				if err != nil {
					return "", err
				}
				project, err := repos.Projects.Get(ctx, session.ProjectID)
				if err != nil {
					return "", err
				}
				return security.EffectiveApprovalMode(config.settings.ApprovalMode, project.ApprovalMode), nil
			},
		},
		HookDispatcher: hooks,
		Logger:         resources.logger,
		ConfigMu:       configMu,
		Events:         events,
		ObserveUsage:   usageObserver(resources.store, resources.logger),
		SummaryFormat:  defaultSummaryFormat(),
		Runner:         runner,
	})
}
