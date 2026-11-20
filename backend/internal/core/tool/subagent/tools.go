package subagent

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/core/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"time"
)

const waitTimeout = 30 * time.Second

var errWaitTimeout = errors.New("subagent wait timed out")

type Assignment struct {
	Task         string          `json:"task"`
	AllowedTools []tool.ToolName `json:"allowedTools"`
}

type IDs struct {
	AgentID identity.AgentID `json:"agentId"`
	RunID   identity.RunID   `json:"runId"`
}

type waitArguments struct {
	IDs  []identity.AgentID `json:"ids"`
	Mode string             `json:"mode"`
}

type waitResult struct {
	Status   string                  `json:"status"`
	Mode     string                  `json:"mode"`
	AgentIDs []identity.AgentID      `json:"agentIds"`
	Reports  []entry.SubagentReceipt `json:"reports"`
}

type sendArguments struct {
	ID      identity.AgentID `json:"id"`
	Message string           `json:"message"`
}

type cancelArguments struct {
	ID identity.AgentID `json:"id"`
}

// ParentOperations 只提供主 Run 的子代理协作操作。
type ParentOperations struct {
	Start  func(context.Context, Assignment, string) (IDs, error)
	Wait   func(context.Context, []identity.AgentID, bool) ([]entry.SubagentReceipt, error)
	Send   func(context.Context, identity.AgentID, string) (string, error)
	Cancel func(identity.AgentID) error
}

func MainTools(ops ParentOperations) []tool.Tool {
	return []tool.Tool{
		tool.Function[Assignment]{
			Name:        "subagent_start",
			Description: "异步委派一个完整子任务。allowedTools 必须显式指定要授权给子代理的工具，工具须在 list_tools 中标记为可委派。无需授权工具时传空数组。子代理只属于本次执行。",
			InputSchema: []byte(`{"type":"object","properties":{"task":{"type":"string"},"allowedTools":{"type":"array","items":{"type":"string"}}},"required":["task","allowedTools"],"additionalProperties":false}`),
			PrepareArguments: func(args Assignment) (Assignment, error) {
				if args.Task == "" || args.AllowedTools == nil {
					return args, errors.New("task and allowedTools are required")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args Assignment, env tool.Environment) (tool.ToolResult, error) {
				return tool.JSONResult(ops.Start(ctx, args, env.CallID))
			},
		},
		tool.Function[waitArguments]{
			Name:        "subagent_wait",
			Description: fmt.Sprintf("等待指定子代理。any 收到新报告或目标全部结束时返回，all 等待目标全部结束。每次最多等待 %d 秒。ready 返回报告引用，正文在后续上下文中提供。timed_out 表示本次等待到期，尚未结束的子代理继续运行。", waitTimeout/time.Second),
			InputSchema: []byte(`{"type":"object","properties":{"ids":{"type":"array","minItems":1,"items":{"type":"string"}},"mode":{"type":"string","enum":["any","all"]}},"required":["ids","mode"],"additionalProperties":false}`),
			PrepareArguments: func(args waitArguments) (waitArguments, error) {
				if len(args.IDs) == 0 {
					return args, errors.New("at least one subagent is required")
				}
				if args.Mode != "any" && args.Mode != "all" {
					return args, errors.New("mode must be any or all")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args waitArguments, _ tool.Environment) (tool.ToolResult, error) {
				waitCtx, cancel := context.WithTimeoutCause(ctx, waitTimeout, errWaitTimeout)
				defer cancel()
				reports, err := ops.Wait(waitCtx, args.IDs, args.Mode == "all")
				result := waitResult{
					Status:   "ready",
					Mode:     args.Mode,
					AgentIDs: args.IDs,
					Reports:  reports,
				}
				if err == nil {
					return tool.JSONResult(result, nil)
				}
				if !errors.Is(err, context.DeadlineExceeded) || context.Cause(waitCtx) != errWaitTimeout {
					return tool.ToolResult{}, err
				}
				if err := ctx.Err(); err != nil {
					return tool.ToolResult{}, err
				}
				result.Status = "timed_out"
				result.Reports = []entry.SubagentReceipt{}
				return tool.JSONResult(result, nil)
			},
		},
		tool.Function[sendArguments]{
			Name:        "send_to_subagent",
			Description: "向本次执行创建的子代理补充指令。子代理的工具授权范围在创建时确定。",
			InputSchema: []byte(`{"type":"object","properties":{"id":{"type":"string"},"message":{"type":"string"}},"required":["id","message"],"additionalProperties":false}`),
			PrepareArguments: func(args sendArguments) (sendArguments, error) {
				if args.ID == "" || args.Message == "" {
					return args, errors.New("subagent ID and instruction text are required")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args sendArguments, _ tool.Environment) (tool.ToolResult, error) {
				id, err := ops.Send(ctx, args.ID, args.Message)
				return tool.JSONResult(map[string]string{"status": "accepted", "messageId": id}, err)
			},
		},
		tool.Function[cancelArguments]{
			Name:        "subagent_cancel",
			Description: "取消本次执行创建的指定子代理。返回只表示取消已接收。",
			InputSchema: []byte(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
			PrepareArguments: func(args cancelArguments) (cancelArguments, error) {
				if args.ID == "" {
					return args, errors.New("subagent ID is required")
				}
				return args, nil
			},
			Execute: func(_ context.Context, args cancelArguments, _ tool.Environment) (tool.ToolResult, error) {
				return tool.JSONResult(map[string]string{"status": "cancel_requested"}, ops.Cancel(args.ID))
			},
		},
	}
}

type reportArguments struct {
	Message string `json:"message"`
}

func DelegateTools(report func(context.Context, string) error) []tool.Tool {
	return []tool.Tool{
		tool.Function[reportArguments]{
			Name:        "report_to_parent",
			Description: "向委派本子任务的父代理提交有意义的纯文本进展。",
			InputSchema: []byte(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
			PrepareArguments: func(args reportArguments) (reportArguments, error) {
				if args.Message == "" {
					return args, errors.New("report text is required")
				}
				return args, nil
			},
			Execute: func(ctx context.Context, args reportArguments, _ tool.Environment) (tool.ToolResult, error) {
				return tool.JSONResult(map[string]string{"status": "accepted"}, report(ctx, args.Message))
			},
		},
	}
}
