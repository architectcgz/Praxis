// Package toolinvocation owns durable admission and settlement for model tool calls.
package toolinvocation

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"fmt"

	"praxis/internal/repository"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

type ExecutionRepository interface {
	Get(context.Context, contracts.AgentExecutionID) (executionmodel.AgentExecution, error)
}

type SecuritySnapshotRepository interface {
	Get(context.Context, contracts.AgentExecutionID) (contracts.ExecutionSecuritySnapshot, error)
}

type Config struct {
	Transactions      repository.TxRunner
	Executions        ExecutionRepository
	SecuritySnapshots SecuritySnapshotRepository
	Invocations       repository.ToolInvocationRepository
	Catalog           runtimecontract.ToolCatalog
	Clock             system.Clock
	IDs               system.IDGenerator
}

type Service struct {
	tx          repository.TxRunner
	executions  ExecutionRepository
	security    SecuritySnapshotRepository
	invocations repository.ToolInvocationRepository
	catalog     runtimecontract.ToolCatalog
	clock       system.Clock
	ids         system.IDGenerator
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":       config.Transactions,
		"executions":         config.Executions,
		"security snapshots": config.SecuritySnapshots,
		"invocations":        config.Invocations,
		"catalog":            config.Catalog,
	} {
		if value == nil {
			return nil, fmt.Errorf("tool invocation service %s is required", name)
		}
	}
	return &Service{
		tx: config.Transactions, executions: config.Executions,
		security: config.SecuritySnapshots, invocations: config.Invocations,
		catalog: config.Catalog,
		clock:   system.ClockOrDefault(config.Clock), ids: system.IDsOrDefault(config.IDs),
	}, nil
}
