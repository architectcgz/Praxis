package toolexecution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"time"
)

// Execute 在同一次调用中准备参数、授权、等待审批、执行工具并提交回执。
func (scope *Scope) Execute(ctx context.Context, call sessionentry.ToolCall) (sessionentry.ContextEntry, error) {
	if ctx == nil {
		return sessionentry.ContextEntry{}, errors.New("tool execution context is required")
	}
	if scope.awaitApproval == nil {
		return sessionentry.ContextEntry{}, errors.New("tool approval wait callback is required")
	}
	s := scope.service
	if err := ctx.Err(); err != nil {
		return s.appendError(ctx, call, "tool_interrupted", err.Error())
	}
	t, ok := s.deps.Catalog.Get(call.Name)
	if !ok {
		return s.appendError(ctx, call, "tool_not_allowed", "The requested tool is disabled or unregistered.")
	}
	args, err := s.deps.Prepare(t, call.Arguments, scope.workspacePath)
	if err != nil {
		return s.appendError(ctx, call, "invalid_tool_call", err.Error())
	}
	return scope.execute(ctx, call, bytes.Clone(args))
}

func (scope *Scope) execute(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (sessionentry.ContextEntry, error) {
	s := scope.service
	for {
		pending, denied, err := scope.authorize(ctx, call, args)
		if err != nil {
			if ctx.Err() != nil {
				return s.appendError(ctx, call, "tool_interrupted", ctx.Err().Error())
			}
			return sessionentry.ContextEntry{}, err
		}
		if denied != nil {
			return s.appendResult(ctx, call, *denied)
		}
		if pending {
			// 审批等待保留在本次调用栈，参数只准备一次；回调不持有存储锁或工具控制锁。
			if err := scope.awaitApproval(ctx, call); err != nil {
				return sessionentry.ContextEntry{}, err
			}
			// 拒绝和停止会直接提交回执，唤醒后优先消费已经完成的调用。
			result, err := s.deps.Calls.GetResult(ctx, call.Key)
			if err == nil {
				return result, nil
			}
			if !errors.Is(err, fault.ErrNotFound) {
				return sessionentry.ContextEntry{}, err
			}
			continue
		}
		t, ok := s.deps.Catalog.Get(call.Name)
		if !ok {
			return s.appendError(ctx, call, "tool_not_allowed", "The requested tool is disabled or unregistered.")
		}
		observed := observedTool{
			Tool:  s.deps.Bind(t, scope.workspacePath),
			hooks: s.deps.HookDispatcher,
			identity: hook.Identity{
				SessionID:   call.SessionID,
				AgentID:     call.AgentID,
				ExecutionID: call.ExecutionID,
				TurnID:      call.TurnID,
			},
			callID: call.CallID,
		}
		execution := executionmodel.NewExecution(ctx, observed, args)
		err = s.registerAndMarkStarted(ctx, call, args, execution)
		if err != nil {
			execution.Stop()
			if errors.Is(err, errAuthorizationChanged) {
				continue
			}
			if ctx.Err() != nil {
				return s.appendError(ctx, call, "tool_interrupted", ctx.Err().Error())
			}
			if errors.Is(err, errStopRequested) {
				return s.appendError(ctx, call, "tool_interrupted", "The tool call was canceled before execution.")
			}
			return sessionentry.ContextEntry{}, err
		}
		result := execution.Run()
		// 保留取消入口直到回执提交结束；迟到 Stop 不会覆盖结果。
		defer s.unregister(call.Key)
		value := result.ToolResult
		if result.Err != nil {
			if value.Payload == "" {
				code := "tool_execution_failed"
				if errors.Is(result.Err, context.Canceled) || errors.Is(result.Err, context.DeadlineExceeded) {
					code = "tool_interrupted"
				}
				value = tool.NewToolError(code, result.Err.Error())
			} else if value.ErrorClass == "" {
				value.ErrorClass = "tool_execution_failed"
			}
		}
		return s.appendResult(ctx, call, value)
	}
}

func (s *Service) appendError(ctx context.Context, call sessionentry.ToolCall, code, text string) (sessionentry.ContextEntry, error) {
	return s.appendResult(ctx, call, tool.NewToolError(code, text))
}

// appendResult 是唯一的正常回执路径，写入失败只向上传播存储错误。
func (s *Service) appendResult(ctx context.Context, call sessionentry.ToolCall, result tool.ToolResult) (sessionentry.ContextEntry, error) {
	if len(result.Payload) > tool.MaxInlineResultBytes {
		result = tool.NewToolError("tool_execution_failed", "Tool result exceeds the inline size limit.")
	}
	if result.Payload == "" {
		result.Payload = "(empty tool result)"
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	stored, _, err := s.commitResult(commitCtx, call, sessionentry.ContextEntry{
		ID:          "tool-result:" + call.ExecutionID.String() + ":" + call.CallID,
		RequestID:   call.RequestID,
		ExecutionID: call.ExecutionID.String(),
		TurnID:      call.TurnID.String(),
		Role:        sessionentry.RoleTool,
		AuthorKind:  sessionentry.AuthorTool,
		AuthorID:    string(call.Name),
		Content: []sessionentry.Block{{
			Kind:    sessionentry.BlockToolResult,
			CallID:  call.CallID,
			Name:    string(call.Name),
			Text:    result.Payload,
			IsError: result.ErrorClass != "",
		}},
	})
	return stored, err
}

// observedTool 只观察真实调用，不参与授权或结果保存。
type observedTool struct {
	tool.Tool
	hooks    *hook.Dispatcher
	identity hook.Identity
	callID   string
}

func (t observedTool) Call(ctx context.Context, args json.RawMessage) (result tool.ToolResult, err error) {
	ctx, scope := t.hooks.Start(ctx, hook.Operation{
		Identity:    t.identity,
		Kind:        hook.OperationTool,
		Name:        string(t.Definition().Name),
		ReferenceID: t.callID,
	})
	defer func() { scope.End(hook.Result{Err: err, ToolErrorClass: result.ErrorClass}) }()
	return t.Tool.Call(ctx, args)
}
