package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/loop"
	"praxis/internal/core/timing"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"slices"
	"strings"
)

type nativeTool struct {
	definition tool.ToolDefinition
	invoke     func(context.Context, string, string) (any, error)
}

func (t *nativeTool) Definition() tool.ToolDefinition { return t.definition }
func (t *nativeTool) NewArguments() any               { return &json.RawMessage{} }
func (t *nativeTool) Prepare(any, tool.Environment) (tool.PreparedCall, error) {
	return tool.PreparedCall{}, fmt.Errorf("native tools require their bound execution entry")
}

type nativeCatalog struct {
	business    tool.ToolCatalog
	tools       map[tool.ToolName]*nativeTool
	definitions []tool.ToolDefinition
}

func (c *nativeCatalog) Get(name tool.ToolName) (tool.Tool, bool) {
	if t := c.tools[name]; t != nil {
		return t, true
	}
	return c.business.Get(name)
}
func (c *nativeCatalog) List() []tool.ToolDefinition { return slices.Clone(c.definitions) }
func (c *nativeCatalog) add(name, description, schema string, invoke func(context.Context, string, string) (any, error)) error {
	if _, found := c.business.Get(tool.ToolName(name)); found {
		return fmt.Errorf("tool name %s is reserved for native collaboration", name)
	}
	t := &nativeTool{definition: tool.ToolDefinition{Name: tool.ToolName(name), Description: description, InputSchema: json.RawMessage(schema)}, invoke: invoke}
	c.tools[t.definition.Name] = t
	c.definitions = append(c.definitions, t.definition)
	return nil
}
func decodeNative(raw string, target any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("native arguments must contain one JSON object")
	}
	return nil
}
func (s *Coordinator) collaborationTools(run *agentrun.Run, active *activeRun, business tool.ToolCatalog) (*nativeCatalog, error) {
	catalog := &nativeCatalog{business: business, tools: make(map[tool.ToolName]*nativeTool), definitions: business.List()}
	if run.Main() {
		run.BindSubagents(func(ctx context.Context, a agentrun.Assignment, callID string) (agentrun.SubagentStatus, error) {
			return s.startSubagent(ctx, run, active, a, callID)
		})
		if err := catalog.add("subagent_start", "异步委派一个完整子任务。必须显式给出工具授权子集和 1–600 秒超时；子代理只属于当前 Run。", `{"type":"object","properties":{"task":{"type":"string"},"allowedTools":{"type":"array","items":{"type":"string"}},"timeoutSeconds":{"type":"integer","minimum":1,"maximum":600}},"required":["task","allowedTools","timeoutSeconds"],"additionalProperties":false}`, func(ctx context.Context, callID, raw string) (any, error) {
			var a agentrun.Assignment
			if err := decodeNative(raw, &a); err != nil {
				return nil, err
			}
			return run.StartSubagent(ctx, a, callID)
		}); err != nil {
			return nil, err
		}
		if err := catalog.add("subagent_status", "查询当前 Run 创建的子代理状态。", `{"type":"object","properties":{"ids":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`, func(ctx context.Context, callID, raw string) (any, error) {
			var a struct {
				IDs []identity.AgentID `json:"ids"`
			}
			if err := decodeNative(raw, &a); err != nil {
				return nil, err
			}
			return run.SubagentStatuses(a.IDs)
		}); err != nil {
			return nil, err
		}
		if err := catalog.add("subagent_wait", "等待指定子代理的新报告或结束，mode 为 any 或 all。返回引用，已接纳报告在后续上下文中提供。", `{"type":"object","properties":{"ids":{"type":"array","minItems":1,"items":{"type":"string"}},"mode":{"type":"string","enum":["any","all"]}},"required":["ids","mode"],"additionalProperties":false}`, func(ctx context.Context, callID, raw string) (any, error) {
			var a struct {
				IDs  []identity.AgentID `json:"ids"`
				Mode string             `json:"mode"`
			}
			if err := decodeNative(raw, &a); err != nil {
				return nil, err
			}
			if a.Mode != "all" && a.Mode != "any" {
				return nil, fmt.Errorf("mode must be any or all")
			}
			return run.WaitSubagents(ctx, a.IDs, a.Mode == "all")
		}); err != nil {
			return nil, err
		}
		if err := catalog.add("send_to_subagent", "向当前 Run 的子代理补充指令，不改变其工具授权。", `{"type":"object","properties":{"id":{"type":"string"},"message":{"type":"string"}},"required":["id","message"],"additionalProperties":false}`, func(ctx context.Context, callID, raw string) (any, error) {
			var a struct {
				ID      identity.AgentID `json:"id"`
				Message string           `json:"message"`
			}
			if err := decodeNative(raw, &a); err != nil {
				return nil, err
			}
			id, err := run.SendToSubagent(ctx, a.ID, a.Message)
			return map[string]any{"status": "accepted", "messageId": id}, err
		}); err != nil {
			return nil, err
		}
		if err := catalog.add("subagent_cancel", "取消当前 Run 创建的指定子代理。返回只表示取消已接收。", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`, func(ctx context.Context, callID, raw string) (any, error) {
			var a struct {
				ID identity.AgentID `json:"id"`
			}
			if err := decodeNative(raw, &a); err != nil {
				return nil, err
			}
			return map[string]string{"status": "cancel_requested"}, run.CancelSubagent(a.ID)
		}); err != nil {
			return nil, err
		}
	} else {
		if err := catalog.add("report_to_parent", "向创建本子任务的父 Run 提交有意义的纯文本进展。", `{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`, func(ctx context.Context, callID, raw string) (any, error) {
			var a struct {
				Message string `json:"message"`
			}
			if err := decodeNative(raw, &a); err != nil {
				return nil, err
			}
			return map[string]string{"status": "accepted"}, run.ReportToParent(ctx, a.Message)
		}); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(catalog.definitions, func(a, b tool.ToolDefinition) int { return strings.Compare(string(a.Name), string(b.Name)) })
	return catalog, nil
}

type nativeExecutor struct {
	run     *agentrun.Run
	emitter *coreevent.Emitter
	base    loop.ToolExecutor
	catalog *nativeCatalog
}

func (e *nativeExecutor) Execute(ctx context.Context, call sessionentry.ToolCall) (toolexecution.CallResult, error) {
	native := e.catalog.tools[call.Name]
	if native == nil {
		return e.base.Execute(ctx, call)
	}
	span := timing.NewSpan()
	span.Start()
	e.emitter.Emit(coreevent.Event{Source: e.run.EventSource(call.TurnID), Payload: coreevent.ToolStarted{CallID: call.CallID, Timing: span.Snapshot()}})
	if call.Name == "subagent_wait" {
		e.run.BeginWaiting()
		defer e.run.EndWaiting()
	}
	value, err := native.invoke(ctx, call.CallID, call.Arguments)
	measured := span.End()
	if err != nil {
		if loop.FailureClassification(err) == loop.FailureStorage {
			return toolexecution.CallResult{}, err
		}
		return toolexecution.CallResult{ToolResult: tool.NewToolError("native_tool_failed", err.Error()), Timing: measured}, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return toolexecution.CallResult{}, err
	}
	return toolexecution.CallResult{ToolResult: tool.NewToolSuccess(string(data), false), Timing: measured}, nil
}
func (e *nativeExecutor) BoundResult(result tool.ToolResult, limit int) (tool.ToolResult, error) {
	return e.base.(interface {
		BoundResult(tool.ToolResult, int) (tool.ToolResult, error)
	}).BoundResult(result, limit)
}
