// Package runtime 编排 Agent 执行，持有 task 和 queue 子域，并在事务提交后发送执行信号。
package runtime

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/core/identity"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/service/runtime/queue"
	"praxis/internal/service/task"
)

// TaskActivator 把已提交的 Task 交给进程内执行协调器。
type TaskActivator interface {
	Activate(context.Context, taskmodel.Task) error
	ResumeApproved(context.Context, taskmodel.Task) error
}

// TaskStarter 是 runtime 用例所需的 Task 准入与收敛能力。
type TaskStarter interface {
	SendInput(context.Context, task.SendInputParams) (task.SendInputResult, error)
	// Abort 收敛激活失败后无人执行的 Task。
	Abort(context.Context, identity.TaskID) error
}

// Config 提供回合、队列和进程内执行依赖；所有依赖均为必填。
type Config struct {
	Approvals ToolApprovals
	Tasks     TaskStarter
	Queue     *queue.Service
	Activator TaskActivator
}

// Service 负责 Task 开始及队列调度，执行控制由 Agent 模块提供。
type Service struct {
	approvals ToolApprovals
	tasks     TaskStarter
	queue     *queue.Service
	activator TaskActivator
}

// StartResult 区分回合创建结果与提交后的执行通知失败。
type StartResult struct {
	task.SendInputResult
	ActivationError string
}

// EnqueueResult 返回预约的 Task 及重复请求状态，入队不触发执行。
type EnqueueResult = queue.EnqueueResult

// NewService 创建 runtime 业务服务；缺少依赖时返回错误。
func NewService(config Config) (*Service, error) {
	for name, missing := range map[string]bool{
		"tasks":          config.Tasks == nil,
		"queue":          config.Queue == nil,
		"activator":      config.Activator == nil,
		"tool approvals": config.Approvals.DecideOperation == nil || config.Approvals.Transactions == nil || config.Approvals.Tasks == nil || config.Approvals.Executions == nil,
	} {
		if missing {
			return nil, fmt.Errorf("runtime service %s is required", name)
		}
	}
	return &Service{
		tasks:     config.Tasks,
		approvals: config.Approvals,
		queue:     config.Queue,
		activator: config.Activator,
	}, nil
}

// SendInput 创建 Task 后开始执行；重复请求返回已有 Task，不重复激活。
func (s *Service) SendInput(ctx context.Context, params task.SendInputParams) (StartResult, error) {
	result, err := s.tasks.SendInput(ctx, params)
	if err != nil {
		return StartResult{}, err
	}
	return s.activate(ctx, result), nil
}

func (s *Service) activate(ctx context.Context, result task.SendInputResult) StartResult {
	response := StartResult{SendInputResult: result}
	if result.ExistingRequest {
		return response
	}
	// 执行不跟随请求 Context 结束；激活失败必须收敛 Task，避免留下已持久化但无人执行的执行中状态。
	ctx = context.WithoutCancel(ctx)
	if err := s.activator.Activate(ctx, result.Task); err != nil {
		response.ActivationError = err.Error()
		if abortErr := s.tasks.Abort(ctx, result.Task.ID); abortErr != nil {
			response.ActivationError = errors.Join(err, abortErr).Error()
		}
	}
	return response
}

// EnqueueTask 只保存当前 runtime 执行期间预约的 Task；当前 Task 结束后由 runtime 领取。
func (s *Service) EnqueueTask(ctx context.Context, params queue.EnqueueParams) (EnqueueResult, error) {
	return s.queue.EnqueueTask(ctx, params)
}
