package bindings

import (
	"praxis/internal/core/approval"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/validation"
	"strings"
	"time"
)

type CommandBindings struct{ host Host }

type CreateTaskRequest struct {
	SubmissionID string `json:"submissionId" validate:"id"`
	SessionID    string `json:"sessionId" validate:"session_id"`
	Prompt       string `json:"prompt" validate:"task_prompt"`
}

type CreateTaskResponse struct {
	TaskID          string `json:"taskId"`
	SchedulingError string `json:"schedulingError,omitempty"`
}

type CancelTaskRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	TaskID    string `json:"taskId" validate:"id"`
}

// QueuedTask 展示内存待办与正文，不携带执行终态。
type QueuedTask struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Sequence  uint64    `json:"sequence"`
	CreatedAt time.Time `json:"createdAt"`
}

type StopRunRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	RunID     string `json:"runId" validate:"id"`
}

type ToolDecisionRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	AgentID   string `json:"agentId" validate:"agent_id"`
	RunID     string `json:"runId" validate:"id"`
	CallID    string `json:"callId" validate:"id"`
}

type ToolApproval struct {
	ToolDecisionRequest
	Name        string `json:"name"`
	RuleID      string `json:"ruleId"`
	Arguments   string `json:"arguments"`
	Action      string `json:"action"`
	RiskVerdict string `json:"riskVerdict"`
	RiskReason  string `json:"riskReason"`
}

type ToolCancelRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	AgentID   string `json:"agentId" validate:"agent_id"`
	RunID     string `json:"runId" validate:"id"`
	CallID    string `json:"callId" validate:"id"`
}

// CreateTask 在首次提交时取得会话写入绑定，后续输入复用同一绑定。
func (b *CommandBindings) CreateTask(wire CreateTaskRequest) (CreateTaskResponse, error) {
	wire.Prompt = strings.TrimSpace(wire.Prompt)
	if err := validation.Struct(wire); err != nil {
		return CreateTaskResponse{}, publicInputError(b.host, "CommandBindings.CreateTask.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return CreateTaskResponse{}, err
	}
	input := taskmodel.Input{
		SubmissionID: identity.TaskID(wire.SubmissionID),
		SessionID:    identity.SessionID(wire.SessionID),
		Prompt:       wire.Prompt,
	}
	result, err := services.Sessions.SubmitTask(ctx, input)
	if err != nil {
		return CreateTaskResponse{}, publicError(b.host, "CommandBindings.CreateTask", err)
	}
	return CreateTaskResponse{
		TaskID:          result.Task.ID.String(),
		SchedulingError: result.SchedulingError,
	}, nil
}

func (b *CommandBindings) StopRun(wire StopRunRequest) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.StopRun.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CommandBindings.StopRun", services.Sessions.StopRun(ctx, identity.SessionID(wire.SessionID), identity.RunID(wire.RunID)))
}

// CancelTask 只移除该 Session 尚未开始的输入，不影响当前 Loop。
func (b *CommandBindings) CancelTask(wire CancelTaskRequest) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.CancelTask.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CommandBindings.CancelTask", services.Sessions.CancelTask(ctx, identity.SessionID(wire.SessionID), identity.TaskID(wire.TaskID)))
}

func (b *CommandBindings) PauseQueue(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "PauseQueue", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "PauseQueue", services.Sessions.PauseQueue(ctx, identity.SessionID(sessionID)))
}

func (b *CommandBindings) ResumeQueue(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "ResumeQueue", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "ResumeQueue", services.Sessions.ResumeQueue(ctx, identity.SessionID(sessionID)))
}

func publicToolApprovals(values []approval.Approval) []ToolApproval {
	result := make([]ToolApproval, 0, len(values))
	for _, value := range values {
		review := value.Review
		result = append(result, ToolApproval{
			ToolDecisionRequest: ToolDecisionRequest{
				SessionID: value.Call.SessionID.String(),
				AgentID:   value.Call.AgentID.String(),
				RunID:     value.Call.RunID.String(),
				CallID:    value.Call.CallID,
			},
			Name:        string(value.Call.Name),
			Arguments:   string(value.Arguments),
			RuleID:      value.Decision.RuleID,
			Action:      string(value.Decision.Action),
			RiskVerdict: string(review.Assessment.Verdict),
			RiskReason:  review.Assessment.Reason,
		})
	}
	return result
}

func (b *CommandBindings) ApproveToolCall(wire ToolDecisionRequest) error {
	return b.decideToolCall(wire, true)
}

func (b *CommandBindings) DenyToolCall(wire ToolDecisionRequest) error {
	return b.decideToolCall(wire, false)
}

func (b *CommandBindings) decideToolCall(wire ToolDecisionRequest, approve bool) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.ToolDecision.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}

	params := sessionservice.ToolDecision{
		SessionID: identity.SessionID(wire.SessionID),
		AgentID:   identity.AgentID(wire.AgentID),
		RunID:     identity.RunID(wire.RunID),
		CallID:    wire.CallID,
	}
	if approve {
		err = services.Sessions.ApproveToolCall(ctx, params)
	} else {
		err = services.Sessions.DenyToolCall(ctx, params)
	}
	if err != nil {
		return publicError(b.host, "CommandBindings.ToolDecision", err)
	}
	return nil
}

func (b *CommandBindings) StopToolCall(wire ToolCancelRequest) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.StopToolCall.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	request := sessionservice.ToolControl{
		SessionID: identity.SessionID(wire.SessionID),
		AgentID:   identity.AgentID(wire.AgentID),
		RunID:     identity.RunID(wire.RunID),
		CallID:    wire.CallID,
	}
	return publicError(b.host, "CommandBindings.StopToolCall", services.Sessions.StopToolCall(ctx, request))
}
