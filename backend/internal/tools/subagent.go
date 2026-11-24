package tools

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

const waitTimeout = 30 * time.Second

var errWaitTimeout = errors.New("subagent wait timed out")

type SubagentAssignment struct {
	Task         string     `json:"task"`
	AllowedTools []ToolName `json:"allowedTools"`
}

type SubagentIDs struct {
	AgentID identity.AgentID `json:"agentId"`
	RunID   identity.RunID   `json:"runId"`
}

// SubagentReceipt 只交付报告引用，正文由父 Run 另行接纳。
type SubagentReceipt struct {
	ReportID identity.ReportID `json:"reportId"`
	AgentID  identity.AgentID  `json:"agentId"`
	Outcome  string            `json:"outcome"`
	Final    bool              `json:"final"`
}

type SubagentParentOperations struct {
	Start  func(context.Context, SubagentAssignment, string) (SubagentIDs, error)
	Wait   func(context.Context, []identity.AgentID, bool) ([]SubagentReceipt, error)
	Send   func(context.Context, identity.AgentID, string) (identity.EntryID, error)
	Cancel func(identity.AgentID) error
}

type waitArguments struct {
	IDs  []identity.AgentID `json:"ids"`
	Mode string             `json:"mode"`
}

type waitResult struct {
	Status   string             `json:"status"`
	Mode     string             `json:"mode"`
	AgentIDs []identity.AgentID `json:"agentIds"`
	Reports  []SubagentReceipt  `json:"reports"`
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

func SubagentMainTools(ops SubagentParentOperations) []Tool {
	return []Tool{
		Function[SubagentAssignment]{
			Name:         "subagent_start",
			Description:  "异步委派一个完整子任务。allowedTools 必须显式指定要授权给子代理的工具，工具须在 list_tools 中标记为可委派。无需授权工具时传空数组。子代理只属于本次执行。",
			InputSchema:  []byte(`{"type":"object","properties":{"task":{"type":"string"},"allowedTools":{"type":"array","items":{"type":"string"}}},"required":["task","allowedTools"],"additionalProperties":false}`),
			SkipApproval: true,
			PrepareArguments: func(args SubagentAssignment) (SubagentAssignment, error) {
				if args.Task == "" || args.AllowedTools == nil {
					return args, errors.New("task and allowedTools are required")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args SubagentAssignment, env Environment) (ToolResult, error) {
				return encodeSubagentResult(ops.Start(ctx, args, env.CallID))
			},
		},
		Function[waitArguments]{
			Name:         "subagent_wait",
			Description:  fmt.Sprintf("等待指定子代理。any 收到新报告或目标全部结束时返回，all 等待目标全部结束。每次最多等待 %d 秒。ready 返回报告引用，正文在后续上下文中提供。timed_out 表示本次等待到期，尚未结束的子代理继续运行。", waitTimeout/time.Second),
			InputSchema:  []byte(`{"type":"object","properties":{"ids":{"type":"array","minItems":1,"items":{"type":"string"}},"mode":{"type":"string","enum":["any","all"]}},"required":["ids","mode"],"additionalProperties":false}`),
			SkipApproval: true,
			PrepareArguments: func(args waitArguments) (waitArguments, error) {
				if len(args.IDs) == 0 {
					return args, errors.New("at least one subagent is required")
				}
				if args.Mode != "any" && args.Mode != "all" {
					return args, errors.New("mode must be any or all")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args waitArguments, _ Environment) (ToolResult, error) {
				return waitForSubagents(ctx, ops.Wait, args)
			},
		},
		Function[sendArguments]{
			Name:         "send_to_subagent",
			Description:  "向当前 Run 的子代理补充指令，不改变其工具授权。",
			InputSchema:  []byte(`{"type":"object","properties":{"id":{"type":"string"},"message":{"type":"string"}},"required":["id","message"],"additionalProperties":false}`),
			SkipApproval: true,
			PrepareArguments: func(args sendArguments) (sendArguments, error) {
				if args.ID == "" || args.Message == "" {
					return args, errors.New("subagent ID and instruction text are required")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args sendArguments, _ Environment) (ToolResult, error) {
				id, err := ops.Send(ctx, args.ID, args.Message)
				return encodeSubagentResult(map[string]string{"status": "accepted", "messageId": id.String()}, err)
			},
		},
		Function[cancelArguments]{
			Name:         "subagent_cancel",
			Description:  "取消当前 Run 创建的指定子代理。返回只表示取消已接收。",
			InputSchema:  []byte(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
			SkipApproval: true,
			PrepareArguments: func(args cancelArguments) (cancelArguments, error) {
				if args.ID == "" {
					return args, errors.New("subagent ID is required")
				}
				return args, nil
			},
			Execute: func(_ context.Context, args cancelArguments, _ Environment) (ToolResult, error) {
				return encodeSubagentResult(map[string]string{"status": "cancel_requested"}, ops.Cancel(args.ID))
			},
		},
	}
}

func waitForSubagents(ctx context.Context, wait func(context.Context, []identity.AgentID, bool) ([]SubagentReceipt, error), args waitArguments) (ToolResult, error) {
	waitCtx, cancel := context.WithTimeoutCause(ctx, waitTimeout, errWaitTimeout)
	defer cancel()
	reports, err := wait(waitCtx, args.IDs, args.Mode == "all")
	result := waitResult{
		Status:   "ready",
		Mode:     args.Mode,
		AgentIDs: args.IDs,
		Reports:  reports,
	}
	if errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(waitCtx), errWaitTimeout) {
		if err := ctx.Err(); err != nil {
			return ToolResult{}, err
		}
		result.Status = "timed_out"
		result.Reports = []SubagentReceipt{}
		return JSONResult(result, nil)
	}
	return encodeSubagentResult(result, err)
}

func SubagentDelegateTools(report func(context.Context, string) error) []Tool {
	return []Tool{
		Function[reportArguments]{
			Name:         "report_to_parent",
			Description:  "向创建本子任务的父 Run 提交有意义的纯文本进展。",
			InputSchema:  []byte(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
			SkipApproval: true,
			PrepareArguments: func(args reportArguments) (reportArguments, error) {
				if args.Message == "" {
					return args, errors.New("report text is required")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args reportArguments, _ Environment) (ToolResult, error) {
				return encodeSubagentResult(map[string]string{"status": "accepted"}, report(ctx, args.Message))
			},
		},
	}
}

func encodeSubagentResult(value any, err error) (ToolResult, error) {
	if err != nil && !errors.Is(err, errorcode.ErrStorage) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return NewToolError("native_tool_failed", err.Error()), nil
	}
	return JSONResult(value, err)
}
