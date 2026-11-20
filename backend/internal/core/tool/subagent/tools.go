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

func NewStart(start func(context.Context, Assignment, string) (IDs, error)) tool.Tool {
	return tool.NewFunction(tool.ToolDefinition{
		Name:        "subagent_start",
		Description: "异步委派一个完整子任务。allowedTools 必须显式指定要授权给子代理的工具，工具须在 list_tools 中标记为可委派。无需授权工具时传空数组。子代理只属于本次执行。",
		InputSchema: []byte(`{"type":"object","properties":{"task":{"type":"string"},"allowedTools":{"type":"array","items":{"type":"string"}}},"required":["task","allowedTools"],"additionalProperties":false}`),
	}, false, func(args Assignment) error {
		if args.Task == "" || args.AllowedTools == nil {
			return errors.New("task and allowedTools are required")
		}
		return nil
	}, func(ctx context.Context, args Assignment, env tool.Environment) (IDs, error) {
		return start(ctx, args, env.CallID)
	})
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

func NewWait(wait func(context.Context, []identity.AgentID, bool) ([]entry.SubagentReceipt, error)) tool.Tool {
	return tool.NewFunction(tool.ToolDefinition{
		Name:        "subagent_wait",
		Description: fmt.Sprintf("等待指定子代理。any 收到新报告或目标全部结束时返回，all 等待目标全部结束。每次最多等待 %d 秒。ready 返回报告引用，正文在后续上下文中提供。timed_out 表示本次等待到期，尚未结束的子代理继续运行。", waitTimeout/time.Second),
		InputSchema: []byte(`{"type":"object","properties":{"ids":{"type":"array","minItems":1,"items":{"type":"string"}},"mode":{"type":"string","enum":["any","all"]}},"required":["ids","mode"],"additionalProperties":false}`),
	}, false, func(args waitArguments) error {
		if len(args.IDs) == 0 {
			return errors.New("at least one subagent is required")
		}
		if args.Mode != "any" && args.Mode != "all" {
			return errors.New("mode must be any or all")
		}
		return nil
	}, func(ctx context.Context, args waitArguments, _ tool.Environment) (waitResult, error) {
		waitCtx, cancel := context.WithTimeoutCause(ctx, waitTimeout, errWaitTimeout)
		defer cancel()
		reports, err := wait(waitCtx, args.IDs, args.Mode == "all")
		result := waitResult{
			Status:   "ready",
			Mode:     args.Mode,
			AgentIDs: args.IDs,
			Reports:  reports,
		}
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, context.DeadlineExceeded) || context.Cause(waitCtx) != errWaitTimeout {
			return waitResult{}, err
		}
		if err := ctx.Err(); err != nil {
			return waitResult{}, err
		}
		result.Status = "timed_out"
		result.Reports = []entry.SubagentReceipt{}
		return result, nil
	})
}

type sendArguments struct {
	ID      identity.AgentID `json:"id"`
	Message string           `json:"message"`
}

func NewSend(send func(context.Context, identity.AgentID, string) (string, error)) tool.Tool {
	return tool.NewFunction(tool.ToolDefinition{
		Name:        "send_to_subagent",
		Description: "向本次执行创建的子代理补充指令。子代理的工具授权范围在创建时确定。",
		InputSchema: []byte(`{"type":"object","properties":{"id":{"type":"string"},"message":{"type":"string"}},"required":["id","message"],"additionalProperties":false}`),
	}, false, func(args sendArguments) error {
		if args.ID == "" || args.Message == "" {
			return errors.New("subagent ID and instruction text are required")
		}
		return nil
	}, func(ctx context.Context, args sendArguments, _ tool.Environment) (map[string]string, error) {
		id, err := send(ctx, args.ID, args.Message)
		return map[string]string{"status": "accepted", "messageId": id}, err
	})
}

type cancelArguments struct {
	ID identity.AgentID `json:"id"`
}

func NewCancel(cancel func(identity.AgentID) error) tool.Tool {
	return tool.NewFunction(tool.ToolDefinition{
		Name:        "subagent_cancel",
		Description: "取消本次执行创建的指定子代理。返回只表示取消已接收。",
		InputSchema: []byte(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
	}, false, func(args cancelArguments) error {
		if args.ID == "" {
			return errors.New("subagent ID is required")
		}
		return nil
	}, func(_ context.Context, args cancelArguments, _ tool.Environment) (map[string]string, error) {
		return map[string]string{"status": "cancel_requested"}, cancel(args.ID)
	})
}

type reportArguments struct {
	Message string `json:"message"`
}

func NewReport(report func(context.Context, string) error) tool.Tool {
	return tool.NewFunction(tool.ToolDefinition{
		Name:        "report_to_parent",
		Description: "向委派本子任务的父代理提交有意义的纯文本进展。",
		InputSchema: []byte(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
	}, false, func(args reportArguments) error {
		if args.Message == "" {
			return errors.New("report text is required")
		}
		return nil
	}, func(ctx context.Context, args reportArguments, _ tool.Environment) (map[string]string, error) {
		return map[string]string{"status": "accepted"}, report(ctx, args.Message)
	})
}
