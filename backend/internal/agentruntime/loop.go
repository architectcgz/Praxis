package agentruntime

import (
	"context"
	"errors"
	"strings"

	"praxis/internal/core/domain"
)

type loopResult struct {
	outcome     domain.AgentRunOutcome
	failureCode string
}

func (r *Runtime) runLoop(ctx context.Context) loopResult {
	toolCallsUsed := 0
	for {
		// A pause or cancelled parent context wins before another turn starts. Queued
		// steer/follow-up input must be durably cleared so it cannot leak into a later run.
		if result, stop := r.abortResult(); stop {
			if err := r.clearAbortQueues(r.settlementContext()); err != nil {
				return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorStorage)}
			}
			return result
		}

		// Reserve a monotonically increasing turn before building provider input; the
		// resource limit applies to attempted turns, including provider failures.
		r.mu.Lock()
		r.turnCount++
		turnNumber := r.turnCount
		r.mu.Unlock()
		if limit := r.config.Grant.ResourceLimits.MaxTurns; limit > 0 && turnNumber > limit {
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorResourceLimit)}
		}
		// The snapshot is assembled from the durable session projection, making every
		// provider request reflect prior assistant messages, tool results, and queue input.
		run, _, _, err := r.currentSettlement()
		if err != nil {
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorContract)}
		}
		snapshot, err := BuildTurnSnapshot(ctx, r.config, run, turnNumber)
		if err != nil {
			return loopResult{outcome: domain.RunFailed, failureCode: runtimeFailureCode(err, ErrorStorage)}
		}
		// Consume the provider stream completely before recording the assistant turn, so
		// text and requested tools are persisted as one coherent model response.
		stream, err := r.config.ModelStream.Stream(ctx, ModelRequest{Snapshot: snapshot.Snapshot()})
		if err != nil {
			if result, stop := r.abortResult(); stop {
				return result
			}
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorProvider)}
		}
		text, calls, streamErr := consumeModelStream(ctx, stream)
		if streamErr != nil {
			if result, stop := r.abortResult(); stop {
				return result
			}
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorProvider)}
		}
		if result, stop := r.abortResult(); stop && strings.TrimSpace(text) == "" && len(calls) == 0 {
			return result
		}
		// Reject the whole response before persistence when its requested tools exceed the
		// remaining run budget; this prevents partial side effects beyond the Grant.
		if limit := r.config.Grant.ResourceLimits.MaxToolCalls; limit > 0 && toolCallsUsed+len(calls) > limit {
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorResourceLimit)}
		}

		// Persist the assistant output before dispatching tools, so each execution has a
		// durable originating tool call for recovery and audit.
		if err := r.appendAssistant(ctx, text, calls); err != nil {
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorStorage)}
		}
		r.mu.Lock()
		r.turnCount = turnNumber
		r.mu.Unlock()

		if len(calls) > 0 {
			toolCallsUsed += len(calls)
			// Tool results are appended as user-visible context and drive the next model turn.
			if result, stop := r.executeTools(ctx, calls); stop {
				return result
			}
		}

		// A save point first flushes durable state, then incorporates higher-priority steer
		// or follow-up input. Draining either queue requires another model turn.
		drained, err := r.savePoint(ctx)
		if err != nil {
			return loopResult{outcome: domain.RunFailed, failureCode: runtimeFailureCode(err, ErrorStorage)}
		}
		if result, stop := r.abortResult(); stop {
			return result
		}
		// A text-only response with no newly drained work is the stable completion point.
		if len(calls) == 0 && !drained {
			return loopResult{outcome: domain.RunCompleted}
		}
	}
}

func consumeModelStream(ctx context.Context, stream <-chan ModelStreamEvent) (string, []ToolCall, error) {
	var text strings.Builder
	var calls []ToolCall
	for {
		select {
		case <-ctx.Done():
			return text.String(), calls, ctx.Err()
		case event, ok := <-stream:
			if !ok {
				return text.String(), calls, nil
			}
			switch event.Kind {
			case StreamTextDelta:
				text.WriteString(event.Text)
			case StreamToolCall:
				calls = append(calls, cloneToolCall(event.ToolCall))
			case StreamComplete:
				return text.String(), calls, nil
			case StreamError:
				if event.Err == nil {
					return text.String(), calls, &RuntimeError{Code: ErrorProvider, Message: "model stream failed"}
				}
				return text.String(), calls, event.Err
			default:
				return text.String(), calls, &RuntimeError{
					Code:    ErrorProvider,
					Message: "model stream emitted an unknown event",
				}
			}
		}
	}
}

func (r *Runtime) appendAssistant(ctx context.Context, text string, calls []ToolCall) error {
	content := make([]TurnContentBlock, 0, 1+len(calls))
	if text != "" {
		content = append(content, TurnContentBlock{Kind: TurnContentText, Text: text})
	}
	for _, call := range calls {
		content = append(
			content,
			TurnContentBlock{
				Kind:       TurnContentToolUse,
				ToolCallID: call.ID,
				ToolName:   string(call.Name),
				Input:      cloneRaw(call.Input),
			},
		)
	}
	_, err := r.appendSessionEvent(
		ctx,
		SessionEventMessage,
		MessageEvent{Message: TurnMessage{Role: TurnRoleAssistant, Content: content}},
		true,
	)
	return err
}

func (r *Runtime) executeTools(ctx context.Context, calls []ToolCall) (loopResult, bool) {
	results := make([]TurnContentBlock, 0, len(calls))
	for _, call := range calls {
		decision, reason := r.preflight(ctx, call)
		if _, err := r.appendSessionEvent(
			ctx,
			SessionEventToolStarted,
			ToolStartedEvent{
				ToolCallID:  call.ID,
				Name:        string(call.Name),
				Preflight:   decision,
				BlockReason: reason,
			},
			true,
		); err != nil {
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorStorage)}, true
		}
		result := ToolExecutionResult{}
		outcome := "ok"
		errorClass := ""
		if decision == PreflightBlocked {
			outcome = "error"
			errorClass = reason
			result.Content = "tool call was blocked by runtime policy"
		} else {
			execCtx := ToolExecutionContext{
				Grant:          r.config.Grant.Snapshot(),
				Execution:      r.config.Execution.Snapshot(),
				LeaseReference: r.config.LeaseReference,
			}
			result, executeErr := r.config.ToolExecutor.Execute(ctx, cloneToolCall(call), execCtx)
			if result.Content == "" {
				result.Content = result.Output
			}
			if executeErr != nil {
				outcome = "error"
				errorClass = string(ErrorTool)
				if result.Content == "" {
					result.Content = "tool execution failed"
				}
			}
			if ctx.Err() != nil && r.isAbortRequested() {
				outcome = "cancelled"
				if errorClass == "" {
					errorClass = string(ErrorInterrupted)
				}
			}
		}
		if _, err := r.appendSessionEvent(
			r.durableContext(ctx),
			SessionEventToolSettled,
			ToolSettledEvent{
				ToolCallID: call.ID,
				Outcome:    outcome,
				ErrorClass: errorClass,
				SideEffect: result.SideEffect,
			},
			true,
		); err != nil {
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorStorage)}, true
		}
		results = append(
			results,
			TurnContentBlock{
				Kind:       TurnContentToolResult,
				ToolCallID: call.ID,
				Text:       result.Content,
				IsError:    outcome != "ok",
			},
		)
	}
	if _, err := r.appendSessionEvent(
		r.durableContext(ctx),
		SessionEventMessage,
		MessageEvent{Message: TurnMessage{Role: TurnRoleUser, Content: results}},
		true,
	); err != nil {
		return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorStorage)}, true
	}
	if result, stop := r.abortResult(); stop {
		if err := r.clearAbortQueues(r.settlementContext()); err != nil {
			return loopResult{outcome: domain.RunFailed, failureCode: string(ErrorStorage)}, true
		}
		return result, true
	}
	return loopResult{}, false
}

func (r *Runtime) preflight(ctx context.Context, call ToolCall) (PreflightDecision, string) {
	// The order is security-relevant: a valid approval can never compensate for a missing Grant or sandbox boundary.
	if !r.config.Grant.AllowsTool(call.Name) {
		return PreflightBlocked, string(ErrorPolicyBlocked)
	}
	if call.Name == domain.ToolWriteFile && !r.config.Execution.SandboxMode.AllowsWorkspaceWrite() {
		return PreflightBlocked, string(ErrorPolicyBlocked)
	}
	if call.Name == domain.ToolRunCommand {
		if call.RequiresWrite && !r.config.Execution.SandboxMode.AllowsWorkspaceWrite() {
			return PreflightBlocked, string(ErrorPolicyBlocked)
		}
		if call.RequiresNetwork && !r.config.Execution.SandboxMode.AllowsNetwork() {
			return PreflightBlocked, string(ErrorPolicyBlocked)
		}
	}
	if call.Name == domain.ToolWriteFile && !r.config.Grant.AllowsWritePath(call.Path) {
		return PreflightBlocked, string(ErrorPolicyBlocked)
	}
	if (call.Name == domain.ToolReadFile || call.Name == domain.ToolListDir || call.Name == domain.ToolSearchText) &&
		!r.config.Grant.AllowsReadPath(call.Path) {
		return PreflightBlocked, string(ErrorPolicyBlocked)
	}
	if call.Name == domain.ToolSubmitResult && !r.config.Grant.AllowsResult(domain.ResultPermissionAgentResult) {
		return PreflightBlocked, string(ErrorPolicyBlocked)
	}
	if call.Name == domain.ToolSubmitBriefing && !r.config.Grant.AllowsResult(domain.ResultPermissionBriefing) {
		return PreflightBlocked, string(ErrorPolicyBlocked)
	}
	if call.Name == domain.ToolRunCommand && !r.config.Execution.ApprovalMode.SkipsCommandConfirmation() {
		if r.config.Approval == nil {
			return PreflightBlocked, string(ErrorApprovalRequired)
		}
		approved, err := r.config.Approval.ApproveCommand(
			ctx,
			cloneToolCall(call),
			ToolExecutionContext{
				Grant:          r.config.Grant.Snapshot(),
				Execution:      r.config.Execution.Snapshot(),
				LeaseReference: r.config.LeaseReference,
			},
		)
		if err != nil || !approved {
			return PreflightBlocked, string(ErrorApprovalRequired)
		}
	}
	return PreflightAllowed, ""
}

func (r *Runtime) savePoint(ctx context.Context) (bool, error) {
	if ctx.Err() != nil {
		return false, r.clearAbortQueues(r.settlementContext())
	}
	if err := r.flush(ctx); err != nil {
		return false, err
	}
	if r.isAbortRequested() || ctx.Err() != nil {
		return false, r.clearAbortQueues(r.settlementContext())
	}
	return r.drainQueue(ctx)
}

func (r *Runtime) durableContext(ctx context.Context) context.Context {
	if ctx == nil || ctx.Err() != nil {
		return r.settlementContext()
	}
	return ctx
}

func (r *Runtime) abortResult() (loopResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.abortRequested && (r.activeContext == nil || r.activeContext.Err() == nil) {
		return loopResult{}, false
	}
	if r.abortRequested {
		return loopResult{outcome: domain.RunPaused}, true
	}
	return loopResult{outcome: domain.RunInterrupted, failureCode: string(ErrorInterrupted)}, true
}

func (r *Runtime) isAbortRequested() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.abortRequested
}

// runtimeFailureCode preserves a structured runtime error after wrapping and otherwise
// applies the caller's fallback.
func runtimeFailureCode(err error, fallback ErrorCode) string {
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr.Code != "" {
		return string(runtimeErr.Code)
	}
	return string(fallback)
}
