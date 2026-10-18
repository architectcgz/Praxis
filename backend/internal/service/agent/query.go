// Package agent 提供 Agent 状态查询、消息查询和执行控制用例。
package agent

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/core/task"
	"praxis/internal/fault"
	"praxis/internal/identity"
	txrunner "praxis/internal/transaction"
)

// ExecutionControl 只在控制意图持久化成功后传递取消信号。
type ExecutionControl interface {
	Pause(identity.AgentID, identity.AgentExecutionID) bool
	Stop(identity.AgentID, identity.AgentExecutionID) bool
}

// ExecutionReader 只查询 Agent 展示和固定目标控制所需的 Execution 事实。
type ExecutionReader interface {
	Get(context.Context, identity.AgentExecutionID) (Execution, error)
	GetActiveByAgent(context.Context, identity.AgentID) (Execution, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]Execution, error)
}

type QueueReader interface {
	ListByAgent(context.Context, identity.AgentID, int) ([]task.Task, error)
}

type Dependencies struct {
	Transactions     txrunner.Runner
	Agents           Store
	ExecutionRecords ExecutionReader
	Queue            QueueReader
	Controls         ControlStore
	Executions       ExecutionControl
	Messages         messagemodel.Reader
	Clock            clock.Clock
}

type Service struct {
	tx               txrunner.Runner
	agents           Store
	executionRecords ExecutionReader
	queue            QueueReader
	controls         ControlStore
	executions       ExecutionControl
	messages         messagemodel.Reader
	clock            clock.Clock
}

// AgentView 是单个 Agent 的持久化详情视图。
type AgentView struct {
	Agent           Agent
	ActiveExecution *Execution
	Executions      []Execution
	Tasks           []task.Task
	Controls        []AgentControlCommand
}

// NewService 校验查询依赖并创建 Agent 查询服务；缺少仓储或消息读取函数时返回错误。
func NewService(deps Dependencies) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":     deps.Transactions,
		"agents":           deps.Agents,
		"executions":       deps.ExecutionRecords,
		"queue":            deps.Queue,
		"control requests": deps.Controls,
	} {
		if value == nil {
			return nil, fmt.Errorf("agent service %s is required", name)
		}
	}
	if deps.Messages == nil || deps.Executions == nil {
		return nil, errors.New("agent service messages and executions are required")
	}
	return &Service{
		tx:               deps.Transactions,
		agents:           deps.Agents,
		executionRecords: deps.ExecutionRecords,
		queue:            deps.Queue,
		controls:         deps.Controls,
		executions:       deps.Executions,
		messages:         deps.Messages,
		clock:            clock.ClockOrDefault(deps.Clock),
	}, nil
}

// GetAgentView 返回 Agent、执行、预约和控制状态，不包含 workflow 编排状态。
func (s *Service) GetAgentView(ctx context.Context, agentID identity.AgentID, limit int) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	if s.executionRecords == nil || s.controls == nil {
		return AgentView{}, errors.New("agent detail query is unavailable")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return AgentView{}, err
	}
	active, err := s.executionRecords.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, fault.ErrNotFound) {
		active = Execution{}
	} else if err != nil {
		return AgentView{}, fmt.Errorf("load active agent task: %w", err)
	}
	tasks, err := s.executionRecords.ListByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	controls, err := s.controls.ListOpenByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	queued, err := s.queue.ListByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	view := AgentView{
		Tasks:      queued,
		Agent:      agent,
		Executions: tasks,
		Controls:   controls,
	}
	if active.ID != "" {
		activeCopy := active
		view.ActiveExecution = &activeCopy
	}
	return view, nil
}

// ListAgentMessages 读取 Agent 自有消息流。
func (s *Service) ListAgentMessages(ctx context.Context, agentID identity.AgentID, limit int) ([]messagemodel.Message, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	if s.messages == nil {
		return nil, errors.New("agent message query is unavailable")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	stream, err := s.messages.LoadMessages(ctx, agent.SessionID, agent.ID, limit)
	if err != nil {
		return nil, err
	}
	executions, err := s.executionRecords.ListByAgent(ctx, agentID, 0)
	if err != nil {
		return nil, err
	}
	sources := make(map[string]string, len(executions))
	for _, execution := range executions {
		if execution.SourceTaskID != "" {
			sources[execution.SourceTaskID.String()] = execution.ID.String()
		}
	}
	for index := range stream.Messages {
		value := &stream.Messages[index]
		if value.ExecutionID == "" && value.SourceTaskID != "" {
			value.ExecutionID = sources[value.SourceTaskID]
		}
	}

	return stream.Messages, nil
}
