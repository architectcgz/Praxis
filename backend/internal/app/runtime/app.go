// Package runtime 提供输入提交、预约任务和工具审批的应用入口。
package runtime

import (
	"context"
	"sync"

	runtimecontracts "praxis/internal/app/runtime/contracts"
	"praxis/internal/core/identity"
	toolmodel "praxis/internal/core/tool_execution"
	"praxis/internal/request"
	businessruntime "praxis/internal/service/runtime"
	taskqueue "praxis/internal/service/runtime/queue"
	businesstask "praxis/internal/service/task"
)

// App 适配任务业务，不执行工具，也不拥有进程内执行绑定。
type App struct {
	runtime  *businessruntime.Service
	configMu *sync.RWMutex
}

// New 注入任务业务和与配置修改共用的唯一配置锁。
func New(runtime *businessruntime.Service, configMu *sync.RWMutex) *App {
	return &App{runtime: runtime, configMu: configMu}
}

// SendInput 接收 canonical 输入，区分持久化结果与提交后的激活失败。
func (a *App) SendInput(ctx context.Context, canonical request.SendInput) (runtimecontracts.SendInputResult, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	result, err := a.runtime.SendInput(ctx, businesstask.SendInputParams{
		SessionID:      identity.SessionID(canonical.SessionID),
		AgentID:        identity.AgentID(canonical.AgentID),
		RequestID:      identity.RequestID(canonical.RequestID),
		Content:        canonical.Content,
		ProviderID:     canonical.ProviderID,
		ModelID:        canonical.ModelID,
		ReasoningLevel: canonical.ReasoningLevel,
	})
	if err != nil {
		return runtimecontracts.SendInputResult{}, err
	}
	return runtimecontracts.SendInputResult{
		TaskID:          result.Task.ID.String(),
		ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

// EnqueueTask 保存预约任务，不触发当前执行。
func (a *App) EnqueueTask(ctx context.Context, canonical request.EnqueueTask) (runtimecontracts.EnqueueTaskResult, error) {
	result, err := a.runtime.EnqueueTask(ctx, taskqueue.EnqueueParams{
		ID:             identity.TaskID(canonical.ID),
		RequestID:      identity.RequestID(canonical.RequestID),
		AgentID:        identity.AgentID(canonical.AgentID),
		Prompt:         canonical.Prompt,
		ProviderID:     canonical.ProviderID,
		ModelID:        canonical.ModelID,
		ReasoningLevel: canonical.ReasoningLevel,
	})
	if err != nil {
		return runtimecontracts.EnqueueTaskResult{}, err
	}
	return runtimecontracts.EnqueueTaskResult{
		TaskID:       result.Task.ID.String(),
		Status:       string(result.Task.Status),
		ExistingTask: result.ExistingTask,
	}, nil
}

// ListToolApprovals 返回可展示的当前审批快照，不暴露工具执行实体。
func (a *App) ListToolApprovals(ctx context.Context, agentID string) ([]runtimecontracts.ToolApproval, error) {
	values, err := a.runtime.ListToolApprovals(ctx, identity.AgentID(agentID))
	if err != nil {
		return nil, err
	}
	result := make([]runtimecontracts.ToolApproval, 0, len(values))
	for _, value := range values {
		review := value.LatestReview()
		result = append(result, runtimecontracts.ToolApproval{
			SessionID:           value.SessionID.String(),
			AgentID:             value.AgentID.String(),
			TaskID:              value.TaskID.String(),
			CallID:              value.ProviderToolCallID,
			Name:                string(value.Name),
			ArgumentsDigest:     toolmodel.ArgumentsDigest(value.Name, value.NormalizedArguments),
			RuleID:              value.Decision.RuleID,
			NormalizedArguments: string(value.NormalizedArguments),
			Path:                value.Path,
			Action:              string(value.Decision.Action),
			RiskVerdict:         string(review.Assessment.Verdict),
			RiskReason:          review.Assessment.Reason,
		})
	}
	return result, nil
}

// ApproveToolCall 提交 canonical 批准决定，审批事务和执行唤醒仍由业务层负责。
func (a *App) ApproveToolCall(ctx context.Context, canonical request.ToolDecision) error {
	return a.runtime.ApproveToolCall(ctx, toolDecision(canonical))
}

// DenyToolCall 提交 canonical 拒绝决定，不在应用层执行或重试工具。
func (a *App) DenyToolCall(ctx context.Context, canonical request.ToolDecision) error {
	return a.runtime.DenyToolCall(ctx, toolDecision(canonical))
}

func toolDecision(canonical request.ToolDecision) businessruntime.ToolDecision {
	return businessruntime.ToolDecision{
		SessionID:       identity.SessionID(canonical.SessionID),
		AgentID:         identity.AgentID(canonical.AgentID),
		TaskID:          identity.TaskID(canonical.TaskID),
		CallID:          canonical.CallID,
		ArgumentsDigest: canonical.ArgumentsDigest,
	}
}
