package repository

import (
	"context"

	"praxis/internal/contracts"
	"praxis/internal/core/session"
	toolexecution "praxis/internal/core/tool_execution"
)

// ToolUsage 只统计已提交的唯一额度和工具结果正文。
type ToolUsage struct {
	Calls       int
	OutputBytes int64
}

// ToolExecutionRepository 保存工具执行记录，并据此重建状态；普通消息由 MessageStreams 读写。
// 返回值均为独立副本，工具调用以完整 CallKey 归属，不负责 Task 等业务实体的状态转换。
type ToolExecutionRepository interface {
	// ReserveTool 原子检查固定 Task 额度并预留一次；已预留返回 false，额度耗尽返回错误。
	ReserveTool(context.Context, toolexecution.CallKey) (bool, error)
	// PrepareTool 冻结规范化参数和路径；相同内容重试无副作用，不同内容返回冲突。
	PrepareTool(context.Context, toolexecution.CallKey, toolexecution.Preparation) error
	// RequestToolApproval 追加人工审批等待，也用于使尚未 Started 的旧授权失效。
	RequestToolApproval(context.Context, toolexecution.CallKey) error
	// RecordToolApproval 保存绑定冻结参数摘要的授权；等待人工审批时不能由策略静默批准。
	RecordToolApproval(context.Context, toolexecution.CallKey, contracts.ApprovalRecord) error
	// TryStartTool 独占一次顶层事务，检查状态、执行 authorize 并可靠提交 Started；最多一个调用方取得资格。
	// 不接受外层事务 Context，防止未提交就启动；authorize 必须只读校验当前 Task、策略及权限。
	// authorize 使用传入的事务 Context 访问仓储；失败、取消或提交不明时不得执行工具。
	TryStartTool(context.Context, toolexecution.CallKey, func(context.Context, toolexecution.State) error) (bool, error)
	// AppendToolResult 将正文与结构化终态作为同一条消息提交；重复回执不重复追加或计费。
	AppendToolResult(context.Context, toolexecution.CallKey, session.MessageData, toolexecution.Outcome) (bool, error)
	// GetToolState 根据历史事实重建状态；Started 且无结果的调用返回 unknown，绝不自动重新启动。
	GetToolState(context.Context, toolexecution.CallKey) (toolexecution.State, error)
	// ListToolStates 按声明顺序返回 Session 的全部工具状态，供业务恢复决策使用。
	ListToolStates(context.Context, contracts.SessionID) ([]toolexecution.State, error)
	// ToolUsage 返回某 Task 的已提交工具额度与输出字节，避免业务存储保存重复计数。
	ToolUsage(context.Context, contracts.SessionID, contracts.TaskID) (ToolUsage, error)
}
