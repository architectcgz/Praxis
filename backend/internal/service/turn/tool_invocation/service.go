// Package toolinvocation 负责模型工具调用的持久化准入与结算。
package toolinvocation

import (
	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"

	"context"
	"fmt"

	runtimecontract "praxis/internal/agent_runtime"
	"praxis/internal/repository"
	"praxis/internal/system"
)

type TurnRepository interface {
	Get(context.Context, contracts.TurnID) (turnmodel.Turn, error)
}

type SecuritySnapshotRepository interface {
	Get(context.Context, contracts.TurnID) (contracts.SecuritySnapshot, error)
}

type Config struct {
	Transactions      repository.TxRunner
	Turns             TurnRepository
	SecuritySnapshots SecuritySnapshotRepository
	Invocations       repository.ToolInvocationRepository
	Catalog           runtimecontract.ToolCatalog
	Clock             system.Clock
	IDs               system.IDGenerator
}

type Service struct {
	tx          repository.TxRunner
	turns       TurnRepository
	security    SecuritySnapshotRepository
	invocations repository.ToolInvocationRepository
	toolCatalog runtimecontract.ToolCatalog
	clock       system.Clock
	ids         system.IDGenerator
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":       config.Transactions,
		"turns":              config.Turns,
		"security snapshots": config.SecuritySnapshots,
		"invocations":        config.Invocations,
		"tool catalog":       config.Catalog,
	} {
		if value == nil {
			return nil, fmt.Errorf("tool invocation service %s is required", name)
		}
	}
	return &Service{
		tx: config.Transactions, turns: config.Turns,
		security: config.SecuritySnapshots, invocations: config.Invocations,
		toolCatalog: config.Catalog,
		clock:       system.ClockOrDefault(config.Clock), ids: system.IDsOrDefault(config.IDs),
	}, nil
}
