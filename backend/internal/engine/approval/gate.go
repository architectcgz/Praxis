// Package approval 负责工具调用的授权模式判定、模型风险审核与人工审批等待。
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/engine/hook"
	"praxis/internal/engine/modelcall"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	coreevent "praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/model"
	"praxis/internal/provider"
	"praxis/internal/security"
	"praxis/internal/tools"
	"sync"
	"time"
)

type GateDependencies struct {
	ModelCaller    *modelcall.Caller
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Logf           func(string, ...any)
	CommitTimeout  time.Duration
	ReviewPolicy   ReviewPolicy
}

// RunOwner 提供授权流程所需的只读执行信息，不依赖具体 Run 实现。
type RunOwner interface {
	BeginWaiting()
	EndWaiting()
	Main() bool
	ID() identity.RunID
	SessionID() identity.SessionID
	SourceTaskID() identity.TaskID
	Model() model.Config
	InputText() string
	EventSource(identity.TurnID) coreevent.EventSource
}

// DecisionRequest 将人工审批决定绑定到具体的活跃工具调用。
type DecisionRequest struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RunID     identity.RunID
	CallID    string
}

type RiskReview struct {
	RequestID  identity.ModelRequestID
	ProviderID string
	ModelID    string
	Assessment security.RiskAssessment
}

// Approval 是当前等待人工审批的调用视图，调用方只读；离开审批等待后不再可查。
type Approval struct {
	Call      entry.ToolCall
	Arguments json.RawMessage
	Decision  security.ApprovalDecision
	Review    RiskReview
}

type pendingApproval struct {
	call        entry.ToolCall
	arguments   json.RawMessage
	decision    security.ApprovalDecision
	review      *RiskReview
	hasDecision bool
	approved    bool
	decided     chan struct{}
}

// Gate 管理所有等待人工审批的活跃调用，与工具执行器的状态与锁完全隔离。
type Gate struct {
	deps         GateDependencies
	reviewFormat reviewFormat
	mu           sync.Mutex
	pending      map[tools.CallKey]*pendingApproval
}

func NewGate(deps GateDependencies) *Gate {
	return &Gate{
		deps:         deps,
		reviewFormat: newReviewFormat(deps.ReviewPolicy.TargetReasonChars),
		pending:      make(map[tools.CallKey]*pendingApproval),
	}
}

// RunGate 使用所属 Run 的 Provider，并在每次授权时读取会话当前模式。
type RunGate struct {
	gate        *Gate
	run         RunOwner
	projectPath string
	mode        func() security.ApprovalMode
	provider    provider.Provider
}

func (g *Gate) ForRun(run RunOwner, projectPath string, mode func() security.ApprovalMode, provider provider.Provider) *RunGate {
	return &RunGate{
		gate:        g,
		run:         run,
		projectPath: projectPath,
		mode:        mode,
		provider:    provider,
	}
}

// ListSessionApprovals 返回指定会话当前全部等待人工审批的调用快照，供会话初始加载使用。
func (g *Gate) ListSessionApprovals(ctx context.Context, sessionID identity.SessionID) ([]Approval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make([]Approval, 0)
	for _, item := range g.pending {
		if item.call.SessionID != sessionID || item.hasDecision {
			continue
		}
		value := Approval{
			Call:      item.call,
			Arguments: item.arguments,
			Decision:  item.decision,
		}
		if item.review != nil {
			value.Review = *item.review
		}
		result = append(result, value)
	}
	return result, nil
}

// DecideApproval 记录人工审批结论并唤醒等待中的工具调用；迟到或冲突决定返回 ErrRequestConflict。
func (g *Gate) DecideApproval(ctx context.Context, request DecisionRequest, approve bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	item := g.pending[tools.CallKey{RunID: request.RunID, CallID: request.CallID}]
	if item == nil || item.call.SessionID != request.SessionID || item.call.AgentID != request.AgentID {
		return errorcode.ErrRequestConflict
	}
	if item.hasDecision {
		if item.approved != approve {
			return errorcode.ErrRequestConflict
		}
		return nil
	}
	item.hasDecision = true
	item.approved = approve
	close(item.decided)
	return nil
}

func (g *Gate) removePending(key tools.CallKey, item *pendingApproval) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending[key] == item {
		delete(g.pending, key)
	}
}

// Authorize 在授权开始时读取模式；已开始的审核或审批等待不受后续模式切换影响。
func (r *RunGate) Authorize(
	ctx context.Context,
	call entry.ToolCall,
	args json.RawMessage,
	operationID identity.OperationID,
	appendEntry entry.AppendEntry,
) (*tools.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decision := security.ResolveApprovalDecision(r.mode())
	if decision.Action == security.ApprovalYolo {
		return nil, nil
	}
	if !r.run.Main() {
		denied := tools.NewToolError("subagent_approval_unavailable", "Subagents cannot request approval for this operation.")
		return &denied, nil
	}

	waiting := false
	beginWaiting := func() {
		if !waiting {
			r.run.BeginWaiting()
			waiting = true
		}
	}
	defer func() {
		if waiting {
			r.run.EndWaiting()
		}
	}()

	var review *RiskReview
	if decision.Action == security.ApprovalAuto {
		beginWaiting()
		reviewCtx, cancel := context.WithTimeout(ctx, r.gate.deps.ReviewPolicy.Timeout)
		assessed, reviewErr := r.review(reviewCtx, call, args, decision.RuleID, appendEntry)
		if reviewErr == nil {
			reviewErr = reviewCtx.Err()
		}
		cancel()
		if reviewErr != nil {
			if ctx.Err() != nil && !errors.Is(reviewErr, errorcode.ErrStorage) {
				return nil, ctx.Err()
			}
			return nil, reviewErr
		}
		switch assessed.Assessment.Verdict {
		case security.RiskSafe:
			return nil, nil
		case security.RiskDangerous:
			denied := tools.NewToolError("tool_risk_denied", "Risk review denied this operation: "+assessed.Assessment.Reason)
			return &denied, nil
		default:
			review = &assessed
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item := &pendingApproval{
		call:      call,
		arguments: args,
		decision:  decision,
		review:    review,
		decided:   make(chan struct{}),
	}
	r.gate.mu.Lock()
	r.gate.pending[call.CallKey] = item
	r.gate.mu.Unlock()
	defer func() {
		r.gate.removePending(call.CallKey, item)
		r.gate.deps.Emitter.Emit(coreevent.Event{
			Source:  r.run.EventSource(call.TurnID),
			Payload: coreevent.ToolApprovalResolved{CallID: call.CallID},
		})
	}()

	beginWaiting()
	r.notifyWaiting(ctx, call, args, operationID, decision, review)
	select {
	case <-item.decided:
		if !item.approved {
			denied := tools.NewToolError("tool_approval_denied", "The user denied this tool call.")
			return &denied, nil
		}
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *RunGate) notifyWaiting(
	ctx context.Context,
	call entry.ToolCall,
	args json.RawMessage,
	operationID identity.OperationID,
	decision security.ApprovalDecision,
	review *RiskReview,
) {
	deps := r.gate.deps
	call.Arguments = string(args)
	event := hook.ToolExecution{
		Identity: hook.Identity{
			OperationID:  operationID,
			SourceTaskID: r.run.SourceTaskID(),
			SessionID:    call.SessionID,
			AgentID:      call.AgentID,
			RunID:        call.RunID,
			TurnID:       call.TurnID,
		},
		Call:      call,
		Arguments: string(args),
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deps.CommitTimeout)
	hookErr := deps.HookDispatcher.ToolExecutionWaiting(commitCtx, event)
	cancel()
	if hookErr != nil && deps.Logf != nil {
		deps.Logf("tool execution waiting hook failed: %v", hookErr)
	}
	payload := coreevent.ToolApprovalRequired{
		CallID:    call.CallID,
		Name:      string(call.Name),
		Arguments: string(args),
		RuleID:    decision.RuleID,
		Action:    decision.Action,
	}
	if review != nil {
		payload.RiskVerdict = review.Assessment.Verdict
		payload.RiskReason = review.Assessment.Reason
	}
	deps.Emitter.Emit(coreevent.Event{
		Source:  r.run.EventSource(call.TurnID),
		Payload: payload,
	})
}
