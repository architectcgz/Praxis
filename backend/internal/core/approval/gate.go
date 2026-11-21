// Package approval 负责工具调用的授权模式判定、模型风险审核与人工审批等待。
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/clock"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/modelcall"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"sync"
	"time"
)

type GateDependencies struct {
	ModelCaller    *modelcall.Caller
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Clock          clock.Clock
	Logf           func(string, ...any)
}

// RunOwner 提供授权流程所需的只读执行信息，不依赖具体 Run 实现。
type RunOwner interface {
	BeginWaiting()
	EndWaiting()
	Main() bool
	ID() identity.RunID
	SessionID() identity.SessionID
	SourceTaskID() identity.TaskID
	Model() modelapi.Config
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
	Call      sessionentry.ToolCall
	Arguments json.RawMessage
	Decision  security.ApprovalDecision
	Review    RiskReview
}

type pendingApproval struct {
	call        sessionentry.ToolCall
	arguments   json.RawMessage
	decision    security.ApprovalDecision
	review      *RiskReview
	requestedAt time.Time
	hasDecision bool
	approved    bool
	decided     chan struct{}
}

// Gate 管理所有等待人工审批的活跃调用，与工具执行器的状态与锁完全隔离。
type Gate struct {
	deps    GateDependencies
	mu      sync.Mutex
	pending map[tool.CallKey]*pendingApproval
}

func NewGate(deps GateDependencies) *Gate {
	return &Gate{
		deps:    deps,
		pending: make(map[tool.CallKey]*pendingApproval),
	}
}

// RunGate 将授权门控绑定到单次 Run 的冻结配置与项目范围。
type RunGate struct {
	gate        *Gate
	run         RunOwner
	projectPath string
	mode        security.ApprovalMode
	provider    providerapi.Provider
}

func (g *Gate) ForRun(run RunOwner, projectPath string, mode security.ApprovalMode, provider providerapi.Provider) *RunGate {
	return &RunGate{
		gate:        g,
		run:         run,
		projectPath: projectPath,
		mode:        mode,
		provider:    provider,
	}
}

func (g *Gate) ListApprovals(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID) ([]Approval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make([]Approval, 0)
	for _, item := range g.pending {
		if item.call.SessionID != sessionID || item.call.AgentID != agentID || item.hasDecision {
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
	item := g.pending[tool.CallKey{RunID: request.RunID, CallID: request.CallID}]
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

func (g *Gate) removePending(key tool.CallKey, item *pendingApproval) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending[key] == item {
		delete(g.pending, key)
	}
}

// Authorize 按冻结审批模式决定放行、拒绝或进入人工审批等待；ctx 取消会立即中断审核与等待。
func (r *RunGate) Authorize(
	ctx context.Context,
	call sessionentry.ToolCall,
	args json.RawMessage,
	operationID string,
	appendEntry sessionentry.AppendEntry,
) (*tool.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decision := security.ResolveApprovalDecision(r.mode)
	if decision.Action == security.ApprovalDeny {
		denied := tool.NewToolError("tool_not_allowed", "The requested tool is disabled or unregistered.")
		return &denied, nil
	}
	if !r.run.Main() && (decision.Action == security.ApprovalAsk || decision.Action == security.ApprovalAuto) {
		denied := tool.NewToolError("subagent_approval_unavailable", "This operation requires approval outside the subagent authorization snapshot.")
		return &denied, nil
	}
	if decision.Action == security.ApprovalYolo {
		return nil, nil
	}
	if decision.Action != security.ApprovalAsk && decision.Action != security.ApprovalAuto {
		return nil, errors.New("tool approval decision is invalid")
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
		reviewCtx, cancel := context.WithTimeout(ctx, time.Minute)
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
			denied := tool.NewToolError("tool_risk_denied", "Risk review denied this operation: "+assessed.Assessment.Reason)
			return &denied, nil
		default:
			review = &assessed
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item := &pendingApproval{
		call:        call,
		arguments:   args,
		decision:    decision,
		review:      review,
		requestedAt: r.gate.deps.Clock.Now().UTC(),
		decided:     make(chan struct{}),
	}
	r.gate.mu.Lock()
	r.gate.pending[call.Key] = item
	r.gate.mu.Unlock()
	defer r.gate.removePending(call.Key, item)

	beginWaiting()
	r.notifyWaiting(ctx, call, args, operationID)
	select {
	case <-item.decided:
		if !item.approved {
			denied := tool.NewToolError("tool_approval_denied", "The user denied this tool call.")
			return &denied, nil
		}
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *RunGate) notifyWaiting(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage, operationID string) {
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
	if err := deps.HookDispatcher.ToolExecutionWaiting(context.WithoutCancel(ctx), event); err != nil && deps.Logf != nil {
		deps.Logf("tool execution waiting hook failed: %v", err)
	}
	deps.Emitter.Emit(coreevent.Event{
		Source:  coreevent.SourceForRun(r.run, call.TurnID),
		Payload: coreevent.ToolApprovalRequired{CallID: call.CallID},
	})
}
