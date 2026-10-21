package bootstrap

import (
	"context"
	"praxis/internal/core/hook"
	"praxis/internal/core/security"
	"praxis/internal/identity"
	"praxis/internal/infra/storage"
	"praxis/internal/loop"
	execution "praxis/internal/service/agent/coordinator"
	executionservice "praxis/internal/service/agent/execution"
	sessionservice "praxis/internal/service/session"
	queueservice "praxis/internal/service/task"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
)

func buildExecution(resources storageResources, repos storage.Repositories, config *configuration, sessions *sessionservice.Service, hooks *hook.Dispatcher, events *execution.EventPublisher, configMu *sync.RWMutex, runner *loop.Runner) (*execution.Coordinator, error) {
	active := &execution.ActiveTasks{}
	tasks, err := executionservice.NewService(executionservice.Dependencies{
		Controls:       repos.Controls,
		Transactions:   resources.store,
		HookDispatcher: hooks,
		Logger:         resources.logger,
		EventObserver: func(value executionservice.TerminalEvent) {
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
		Sessions:     repos.Sessions,
		Agents:       repos.Agents,
		Records:      repos.Executions,
		Queue:        repos.Tasks,
		Executions:   active,
		Messages:     repos.Messages,
		PrimaryAgent: sessions,
		Definitions:  config.agents,
		Models:       config.models,
		Contexts:     repos.Contexts,
	})
	if err != nil {
		return nil, err
	}

	queue, err := queueservice.NewService(queueservice.Dependencies{
		Transactions:   resources.store,
		Tasks:          repos.Tasks,
		Agents:         repos.Agents,
		Sessions:       repos.Sessions,
		Messages:       repos.Messages,
		HookDispatcher: hooks,
		Logger:         resources.logger,
	})
	if err != nil {
		return nil, err
	}
	return assembleCoordinator(coordinatorDependencies{
		Transactions: resources.store,
		Tasks:        tasks,
		Requests:     tasks,
		ProgressHook: executionservice.ProgressHook{Service: tasks},
		Records:      repos.Executions,
		Queue:        queue,
		Recovery: execution.RecoveryCatalog{
			Executions: repos.Executions,
			Sessions:   repos.Sessions,
			Agents:     repos.Agents,
		},
		ActiveTasks:    active,
		ToolExecutions: repos.ToolExecutions,
		ToolCalls:      repos.ToolCalls,
		Agents:         repos.Agents,
		Messages:       repos.Messages,
		Sessions:       repos.Sessions,
		Workspaces:     repos.Workspaces,
		Models:         config.models,
		Tools:          config.tools,
		Rules: toolservice.ApprovalRules{
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
		ObserveUsage:   usageObserver(repos.ModelUsage, resources.logger),
		SummaryFormat:  defaultSummaryFormat(),
		Runner:         runner,
	})
}
