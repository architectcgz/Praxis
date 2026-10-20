package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/hook"
	"praxis/internal/core/message"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"time"
)

// Execute 只读取调用声明与控制事实，正常入口不查询已有结果。
func (scope *Scope) Execute(ctx context.Context, executionID identity.AgentExecutionID, callID string) (message.Message, bool, error) {
	if ctx == nil {
		return message.Message{}, false, errors.New("tool execution context is required")
	}
	s := scope.service
	call, err := s.call(ctx, executionID, callID)
	if err != nil {
		if ctx.Err() != nil {
			return s.appendCanceledCall(ctx, executionID, callID)
		}
		return message.Message{}, false, err
	}
	args, err := s.deps.Prepare(call.Name, call.Arguments, scope.workspacePath)
	if err != nil {
		return s.appendError(ctx, call, "invalid_tool_call", err.Error())
	}
	return scope.execute(ctx, call, args)
}

func (scope *Scope) execute(ctx context.Context, call message.ToolCall, args json.RawMessage) (message.Message, bool, error) {
	s := scope.service
	for {
		pending, denied, err := scope.authorize(ctx, call, args)
		if err != nil {
			if ctx.Err() != nil {
				return s.appendError(ctx, call, "tool_interrupted", ctx.Err().Error())
			}
			return message.Message{}, false, err
		}
		if denied != nil {
			return s.appendResult(ctx, call, *denied)
		}
		if pending {
			return message.Message{}, true, nil
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
			return message.Message{}, false, err
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

// Resume 仅用于当前 Loop 的人工审批续接；拒绝已经直接提交回执。
func (scope *Scope) Resume(ctx context.Context, executionID identity.AgentExecutionID, callID string) (message.Message, bool, error) {
	key := executionmodel.Key{ExecutionID: executionID, CallID: callID}
	result, err := scope.service.deps.Messages.ToolResult(ctx, key)
	if err == nil {
		return result, false, nil
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return message.Message{}, false, err
	}
	call, err := scope.service.call(ctx, executionID, callID)
	if err != nil {
		return message.Message{}, false, err
	}
	facts, err := scope.service.deps.Events.Read(ctx, key)
	if err != nil {
		return message.Message{}, false, err
	}
	if facts.RequiredAt.IsZero() {
		return message.Message{}, false, fault.ErrRequestConflict
	}
	// 沿用本次 Loop 已准备并展示的参数，不再次清洗输入。
	return scope.execute(ctx, call, json.RawMessage(facts.Summary))
}

func (s *Service) appendError(ctx context.Context, call message.ToolCall, code, text string) (message.Message, bool, error) {
	return s.appendResult(ctx, call, tool.NewToolError(code, text))
}

// appendResult 是唯一的正常回执路径，写入失败只向上传播存储错误。
func (s *Service) appendResult(ctx context.Context, call message.ToolCall, result tool.ToolResult) (message.Message, bool, error) {
	if len(result.Payload) > tool.MaxInlineResultBytes {
		result = tool.NewToolError("tool_execution_failed", "Tool result exceeds the inline size limit.")
	}
	if result.Payload == "" {
		result.Payload = "(empty tool result)"
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	writer, err := s.deps.Messages.Writer(commitCtx, call.SessionID, call.AgentID)
	if err != nil {
		return message.Message{}, false, err
	}
	stored, _, err := writer.AppendToolResult(commitCtx, message.Message{
		ID:          "tool-result:" + call.ExecutionID.String() + ":" + call.CallID,
		RequestID:   call.RequestID,
		ExecutionID: call.ExecutionID.String(),
		TurnID:      call.TurnID.String(),
		Role:        message.RoleTool,
		AuthorKind:  message.AuthorTool,
		AuthorID:    string(call.Name),
		Blocks: []message.Block{{
			Kind:    message.BlockToolResult,
			CallID:  call.CallID,
			Name:    string(call.Name),
			Text:    result.Payload,
			IsError: result.ErrorClass != "",
		}},
	})
	return stored, false, err
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

// appendCanceledCall 在准入前取消时使用独立提交 Context，保留明确的取消回执。
func (s *Service) appendCanceledCall(ctx context.Context, executionID identity.AgentExecutionID, callID string) (message.Message, bool, error) {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	call, err := s.call(readCtx, executionID, callID)
	if err != nil {
		return message.Message{}, false, err
	}
	return s.appendError(readCtx, call, "tool_interrupted", ctx.Err().Error())
}
