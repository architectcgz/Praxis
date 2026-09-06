// Package toolinvocation owns durable admission and settlement for model tool calls.
package toolinvocation

import (
	"context"
	"fmt"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
	"praxis/internal/persistence"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

type ExecutionRepository interface {
	Get(context.Context, domainfoundation.AgentExecutionID) (domainexecution.AgentExecution, error)
}

type SecuritySnapshotRepository interface {
	Get(context.Context, domainfoundation.AgentExecutionID) (domainsecurity.ExecutionSecuritySnapshot, error)
}

type Config struct {
	Transactions      persistence.TxRunner
	Executions        ExecutionRepository
	SecuritySnapshots SecuritySnapshotRepository
	Invocations       persistence.ToolInvocationRepository
	Catalog           runtimecontract.ToolCatalog
	Executor          runtimecontract.ToolExecutor
	Clock             system.Clock
	IDs               system.IDGenerator
}

type Service struct {
	tx          persistence.TxRunner
	executions  ExecutionRepository
	security    SecuritySnapshotRepository
	invocations persistence.ToolInvocationRepository
	catalog     runtimecontract.ToolCatalog
	executor    runtimecontract.ToolExecutor
	clock       system.Clock
	ids         system.IDGenerator
}

func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{"transactions", config.Transactions},
		{"executions", config.Executions},
		{"security snapshots", config.SecuritySnapshots},
		{"invocations", config.Invocations},
		{"catalog", config.Catalog},
		{"executor", config.Executor},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("tool invocation service %s is required", required.name)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	ids := config.IDs
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	return &Service{
		tx: config.Transactions, executions: config.Executions,
		security: config.SecuritySnapshots, invocations: config.Invocations,
		catalog: config.Catalog, executor: config.Executor, clock: clock, ids: ids,
	}, nil
}
