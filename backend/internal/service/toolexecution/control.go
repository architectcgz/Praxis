package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/message"
	"praxis/internal/core/security"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// 锁顺序固定为 controlMu → 配置读锁 → 仓储写入锁；工具 I/O 不持有这些锁。
// started 和活动登记共用此锁，Stop 持久化成功后才取消，启动间隙不会丢信号。
func (s *Service) registerAndMarkStarted(ctx context.Context, call message.ToolCall, args json.RawMessage, execution *executionmodel.Execution) error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if s.deps.Rules.ConfigMu != nil {
		s.deps.Rules.ConfigMu.RLock()
		defer s.deps.Rules.ConfigMu.RUnlock()
	}
	err := func(txCtx context.Context) error {
		task, err := s.deps.Tasks.Get(txCtx, call.ExecutionID)
		if err != nil {
			return err
		}
		if !task.Active() {
			return fault.ErrRequestConflict
		}
		facts, err := s.deps.Events.Read(txCtx, call.Key)
		if err != nil {
			return err
		}
		if !facts.StopRequestedAt.IsZero() {
			return errStopRequested
		}
		decision, err := s.deps.Rules.decide(txCtx, call, args)
		if err != nil {
			return err
		}
		digest := executionmodel.ArgumentsDigest(call.Name, args)
		if decision.Action == security.ApprovalDeny || facts.Approval.ArgumentsDigest != digest || facts.Approval.ApprovedAt.IsZero() || facts.Decision != decision && !(decision.Action == security.ApprovalAsk && facts.Approval.Source == executionmodel.ApprovalSourceUser) {
			return errAuthorizationChanged
		}
		if err := s.deps.Events.Append(txCtx, executionmodel.Event{
			Key:             call.Key,
			SessionID:       call.SessionID,
			Kind:            executionmodel.Started,
			At:              s.clock.Now().UTC(),
			ArgumentsDigest: digest,
		}); err != nil {
			return err
		}
		return nil
	}(ctx)
	if err == nil {
		s.active[call.Key] = execution
	}
	return err
}

func (s *Service) unregister(key executionmodel.Key) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	delete(s.active, key)
}

// ControlRequest 将停止请求绑定到调用所属的会话和 Agent。
type ControlRequest struct {
	SessionID   identity.SessionID
	AgentID     identity.AgentID
	ExecutionID identity.AgentExecutionID
	CallID      string
}

// Stop 不写完成状态；迟到请求直接保留已提交的回执。
func (s *Service) Stop(ctx context.Context, executionID identity.AgentExecutionID, callID string) error {
	_, err := s.StopCall(ctx, ControlRequest{ExecutionID: executionID, CallID: callID})
	return err
}

// StopCall 返回已校验的执行归属，供 Coordinator 在提交后唤醒审批等待。
func (s *Service) StopCall(ctx context.Context, request ControlRequest) (task agentmodel.Execution, err error) {
	key := executionmodel.Key{ExecutionID: request.ExecutionID, CallID: request.CallID}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	err = func(txCtx context.Context) error {
		call, err := s.call(txCtx, key.ExecutionID, key.CallID)
		if err != nil {
			return err
		}
		if request.SessionID != "" && request.SessionID != call.SessionID || request.AgentID != "" && request.AgentID != call.AgentID {
			return fault.ErrRequestConflict
		}
		task, err = s.deps.Tasks.Get(txCtx, key.ExecutionID)
		if err != nil {
			return err
		}
		if _, err := s.deps.Calls.GetResult(txCtx, key); err == nil {
			return nil
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		facts, err := s.deps.Events.Read(txCtx, key)
		if err != nil {
			return err
		}
		if facts.StopRequestedAt.IsZero() {
			if err := s.deps.Events.Append(txCtx, executionmodel.Event{
				Key:       key,
				SessionID: call.SessionID,
				Kind:      executionmodel.StopRequested,
				At:        s.clock.Now().UTC(),
			}); err != nil {
				return err
			}
		}
		if facts.StartedAt.IsZero() {
			_, err := s.appendError(txCtx, call, "tool_interrupted", "The tool call was canceled before execution.")
			return err
		}
		return nil
	}(ctx)
	if err == nil {
		if execution := s.active[key]; execution != nil {
			execution.Stop()
		}
	}
	return task, err
}
