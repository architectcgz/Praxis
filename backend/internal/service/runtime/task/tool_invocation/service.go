// Package toolinvocation 负责模型工具调用的持久化准入与结算。
package toolinvocation

import (
	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"

	"context"
	"fmt"

	"praxis/internal/agent_runtime"
	"praxis/internal/repository"
	"praxis/internal/system"
)

type TaskRepository interface {
	Get(context.Context, contracts.TaskID) (taskmodel.Task, error)
}

type SecuritySnapshotRepository interface {
	Get(context.Context, contracts.TaskID) (contracts.SecuritySnapshot, error)
}

type Config struct {
	Transactions      repository.TxRunner
	Tasks             TaskRepository
	SecuritySnapshots SecuritySnapshotRepository
	Invocations       repository.ToolInvocationRepository
	Catalog           agentruntime.ToolCatalog
	Clock             system.Clock
	IDs               system.IDGenerator
}

type Service struct {
	tx          repository.TxRunner
	tasks       TaskRepository
	security    SecuritySnapshotRepository
	invocations repository.ToolInvocationRepository
	toolCatalog agentruntime.ToolCatalog
	clock       system.Clock
	ids         system.IDGenerator
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":       config.Transactions,
		"tasks":              config.Tasks,
		"security snapshots": config.SecuritySnapshots,
		"invocations":        config.Invocations,
		"tool catalog":       config.Catalog,
	} {
		if value == nil {
			return nil, fmt.Errorf("tool invocation service %s is required", name)
		}
	}
	return &Service{
		tx: config.Transactions, tasks: config.Tasks,
		security: config.SecuritySnapshots, invocations: config.Invocations,
		toolCatalog: config.Catalog,
		clock:       system.ClockOrDefault(config.Clock), ids: system.IDsOrDefault(config.IDs),
	}, nil
}
