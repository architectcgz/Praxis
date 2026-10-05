package agentruntime

import (
	"context"

	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"
)

// TurnLifecycle 提供 runtime 所需的执行状态回调。
type TurnLifecycle interface {
	StartRuntimeTurn(context.Context, contracts.TurnID) error
	SettleRuntimeTurn(
		context.Context,
		contracts.TurnID,
		turnmodel.TurnOutcome,
		contracts.TurnFailureCode,
		string,
	) error
}
