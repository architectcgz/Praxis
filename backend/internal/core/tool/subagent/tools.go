// Package subagent 定义父子代理协作工具的协议、参数校验与免审批实现。
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// Assignment 描述父 Run 委派给子代理的任务与工具授权范围。
type Assignment struct {
	Task           string          `json:"task"`
	AllowedTools   []tool.ToolName `json:"allowedTools"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
}

// StartResult 是成功启动子代理后返回给模型的身份引用。
type StartResult struct {
	AgentID identity.AgentID `json:"agentId"`
	RunID   identity.RunID   `json:"runId"`
}

// MainRuntime 提供主 Run 所需的子代理编排能力。
type MainRuntime interface {
	StartSubagent(ctx context.Context, callID string, assignment Assignment) (StartResult, error)
	WaitSubagents(ctx context.Context, ids []identity.AgentID, all bool) (any, error)
	SendToSubagent(ctx context.Context, id identity.AgentID, message string) (string, error)
	CancelSubagent(id identity.AgentID) error
	BeginWaiting()
	EndWaiting()
}

// ChildRuntime 提供子 Run 向父 Run 汇报进展的能力。
type ChildRuntime interface {
	ReportToParent(ctx context.Context, message string) error
}

// MainTools 返回绑定到主 Run 运行时能力的子代理协作工具。
func MainTools(runtime MainRuntime) []tool.Tool {
	return []tool.Tool{
		startTool{runtime: runtime},
		waitTool{runtime: runtime},
		sendTool{runtime: runtime},
		cancelTool{runtime: runtime},
	}
}

// ChildTools 返回绑定到子 Run 运行时能力的协作汇报工具。
func ChildTools(runtime ChildRuntime) []tool.Tool {
	return []tool.Tool{
		reportTool{runtime: runtime},
	}
}

type waitArguments struct {
	IDs  []identity.AgentID `json:"ids"`
	Mode string             `json:"mode"`
}

type sendArguments struct {
	ID      identity.AgentID `json:"id"`
	Message string           `json:"message"`
}

type cancelArguments struct {
	ID identity.AgentID `json:"id"`
}

type reportArguments struct {
	Message string `json:"message"`
}

type startTool struct{ runtime MainRuntime }

func (startTool) RequiresApproval() bool { return false }

func (startTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name:        "subagent_start",
		Description: "异步委派一个完整子任务。必须显式给出工具授权子集和 1–600 秒超时；子代理只属于当前 Run。",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"task":{"type":"string"},"allowedTools":{"type":"array","items":{"type":"string"}},"timeoutSeconds":{"type":"integer","minimum":1,"maximum":600}},"required":["task","allowedTools","timeoutSeconds"],"additionalProperties":false}`),
	}
}

func (startTool) NewArguments() any { return &Assignment{} }

func (t startTool) Prepare(arguments any, environment tool.Environment) (tool.PreparedCall, error) {
	input := *arguments.(*Assignment)
	if input.Task == "" || input.AllowedTools == nil || input.TimeoutSeconds < 1 || input.TimeoutSeconds > 600 {
		return tool.PreparedCall{}, errors.New("task, allowedTools and timeoutSeconds (1-600) are required")
	}
	return tool.PreparedCall{
		Arguments: input,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			value, err := t.runtime.StartSubagent(ctx, environment.CallID, input)
			return encodeResult(value, err)
		},
	}, nil
}

type waitTool struct{ runtime MainRuntime }

func (waitTool) RequiresApproval() bool { return false }

func (waitTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name:        "subagent_wait",
		Description: "等待指定子代理的新报告或结束，mode 为 any 或 all。返回引用，已接纳报告在后续上下文中提供。",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"ids":{"type":"array","minItems":1,"items":{"type":"string"}},"mode":{"type":"string","enum":["any","all"]}},"required":["ids","mode"],"additionalProperties":false}`),
	}
}

func (waitTool) NewArguments() any { return &waitArguments{} }

func (t waitTool) Prepare(arguments any, _ tool.Environment) (tool.PreparedCall, error) {
	input := *arguments.(*waitArguments)
	if len(input.IDs) == 0 {
		return tool.PreparedCall{}, errors.New("at least one subagent is required")
	}
	if input.Mode != "all" && input.Mode != "any" {
		return tool.PreparedCall{}, errors.New("mode must be any or all")
	}
	return tool.PreparedCall{
		Arguments: input,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			t.runtime.BeginWaiting()
			defer t.runtime.EndWaiting()
			value, err := t.runtime.WaitSubagents(ctx, input.IDs, input.Mode == "all")
			return encodeResult(value, err)
		},
	}, nil
}

type sendTool struct{ runtime MainRuntime }

func (sendTool) RequiresApproval() bool { return false }

func (sendTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name:        "send_to_subagent",
		Description: "向当前 Run 的子代理补充指令，不改变其工具授权。",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"message":{"type":"string"}},"required":["id","message"],"additionalProperties":false}`),
	}
}

func (sendTool) NewArguments() any { return &sendArguments{} }

func (t sendTool) Prepare(arguments any, _ tool.Environment) (tool.PreparedCall, error) {
	input := *arguments.(*sendArguments)
	if input.ID == "" {
		return tool.PreparedCall{}, errors.New("subagent id is required")
	}
	if input.Message == "" {
		return tool.PreparedCall{}, errors.New("instruction text is required")
	}
	return tool.PreparedCall{
		Arguments: input,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			id, err := t.runtime.SendToSubagent(ctx, input.ID, input.Message)
			return encodeResult(map[string]any{"status": "accepted", "messageId": id}, err)
		},
	}, nil
}

type cancelTool struct{ runtime MainRuntime }

func (cancelTool) RequiresApproval() bool { return false }

func (cancelTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name:        "subagent_cancel",
		Description: "取消当前 Run 创建的指定子代理。返回只表示取消已接收。",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
	}
}

func (cancelTool) NewArguments() any { return &cancelArguments{} }

func (t cancelTool) Prepare(arguments any, _ tool.Environment) (tool.PreparedCall, error) {
	input := *arguments.(*cancelArguments)
	if input.ID == "" {
		return tool.PreparedCall{}, errors.New("subagent id is required")
	}
	return tool.PreparedCall{
		Arguments: input,
		Call: func(context.Context) (tool.ToolResult, error) {
			err := t.runtime.CancelSubagent(input.ID)
			return encodeResult(map[string]string{"status": "cancel_requested"}, err)
		},
	}, nil
}

type reportTool struct{ runtime ChildRuntime }

func (reportTool) RequiresApproval() bool { return false }

func (reportTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name:        "report_to_parent",
		Description: "向创建本子任务的父 Run 提交有意义的纯文本进展。",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
	}
}

func (reportTool) NewArguments() any { return &reportArguments{} }

func (t reportTool) Prepare(arguments any, _ tool.Environment) (tool.PreparedCall, error) {
	input := *arguments.(*reportArguments)
	if input.Message == "" {
		return tool.PreparedCall{}, errors.New("report text is required")
	}
	return tool.PreparedCall{
		Arguments: input,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			err := t.runtime.ReportToParent(ctx, input.Message)
			return encodeResult(map[string]string{"status": "accepted"}, err)
		},
	}, nil
}

func encodeResult(value any, err error) (tool.ToolResult, error) {
	if err != nil {
		if errors.Is(err, errorcode.ErrStorage) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return tool.ToolResult{}, err
		}
		return tool.NewToolError("native_tool_failed", err.Error()), nil
	}
	data, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return tool.ToolResult{}, fmt.Errorf("encode collaboration result: %w", marshalErr)
	}
	return tool.NewToolSuccess(string(data), false), nil
}
