package contracts

import (
	"strings"
)

// TurnFailureCode 是持久化回合和 Wails 共用的稳定失败原因。
type TurnFailureCode string

const (
	TurnFailureProviderUnavailable TurnFailureCode = "provider_unavailable"
	TurnFailureProvider            TurnFailureCode = "turn_provider_error"
	TurnFailureTool                TurnFailureCode = "turn_tool_error"
	TurnFailurePolicyBlocked       TurnFailureCode = "turn_policy_blocked"
	TurnFailureApprovalRequired    TurnFailureCode = "turn_approval_required"
	TurnFailureResourceLimit       TurnFailureCode = "turn_resource_limit"
	TurnFailureStorage             TurnFailureCode = "turn_storage_error"
	TurnFailureContract            TurnFailureCode = "turn_contract_error"
	TurnFailureBusy                TurnFailureCode = "turn_busy"
	TurnFailureInterrupted         TurnFailureCode = "turn_interrupted"
	TurnFailureRequestCanceled     TurnFailureCode = "request_canceled"
	TurnFailureRuntimeCancelled    TurnFailureCode = "runtime_cancelled"
	TurnFailureRuntimeFailed       TurnFailureCode = "runtime_failed"
	TurnFailureRuntimeInvalid      TurnFailureCode = "runtime_invalid_outcome"
)

// MaxTurnFailureMessageRunes 限制可展示的 Provider 失败详情，避免错误响应无限增长。
const MaxTurnFailureMessageRunes = 4096

// TurnFailureMessage 只为 Provider 失败提取可展示详情，其他错误仍由前端按失败码展示。
func TurnFailureMessage(code TurnFailureCode, err error) string {
	if code != TurnFailureProvider || err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) > MaxTurnFailureMessageRunes {
		return string(runes[:MaxTurnFailureMessageRunes]) + "..."
	}
	return message
}

func (c TurnFailureCode) Valid() bool {
	switch c {
	case "", TurnFailureProviderUnavailable, TurnFailureProvider,
		TurnFailureTool, TurnFailurePolicyBlocked,
		TurnFailureApprovalRequired, TurnFailureResourceLimit,
		TurnFailureStorage, TurnFailureContract, TurnFailureBusy,
		TurnFailureInterrupted, TurnFailureRequestCanceled,
		TurnFailureRuntimeCancelled, TurnFailureRuntimeFailed,
		TurnFailureRuntimeInvalid:
		return true
	default:
		return false
	}
}
