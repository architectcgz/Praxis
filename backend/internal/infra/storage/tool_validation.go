package storage

import (
	"encoding/json"
	"errors"
	"praxis/internal/core/security"
	"praxis/internal/core/toolexecution"
	"strings"
	"unicode/utf8"
)

// validateToolEvent 在写入和磁盘恢复边界严格验证当前事实 Schema。
func validateToolEvent(e toolexecution.Event) error {
	for _, value := range []string{e.ExecutionID.String(), e.SessionID.String(), e.CallID} {
		if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("tool event identity must be canonical and nonempty")
		}
	}
	if e.At.IsZero() {
		return errors.New("tool event time is required")
	}
	validOperation := func(args json.RawMessage, decision security.ApprovalDecision) bool {
		return len(args) > 0 && json.Valid(args) && decision.Action.Valid() && decision.RuleID != "" && decision.RuleID == strings.TrimSpace(decision.RuleID)
	}
	switch e.Kind {
	case toolexecution.ApprovalRequired:
		v := e.Request
		if v == nil || e.UserDecision != nil || e.Review != nil || v.Key != e.Key || v.SessionID != e.SessionID || !v.RequestedAt.Equal(e.At) || !validOperation(v.Arguments, v.Decision) || v.Decision.Action != security.ApprovalAsk && v.Decision.Action != security.ApprovalAuto {
			return errors.New("approval request must contain matching identity, time, arguments and rule")
		}
	case toolexecution.ApprovalDecided:
		v := e.UserDecision
		if v == nil || e.Request != nil || e.Review != nil || v.Key != e.Key || v.SessionID != e.SessionID || !v.DecidedAt.Equal(e.At) {
			return errors.New("user decision must reference a matching approval request")
		}
	case toolexecution.Reviewed:
		v := e.Review
		if v == nil || e.Request != nil || e.UserDecision != nil || v.Key != e.Key || v.SessionID != e.SessionID || !validOperation(v.Arguments, v.Decision) || v.Decision.Action != security.ApprovalAuto {
			return errors.New("risk review must include matching identity, arguments and rule")
		}
		if v.TurnID == "" || v.ProviderID == "" || v.ModelID == "" || v.StartedAt.IsZero() || v.CompletedAt.Before(v.StartedAt) || !e.At.Equal(v.CompletedAt) {
			return errors.New("risk review identity and times are invalid")
		}
		if v.Assessment.Verdict != security.RiskSafe && v.Assessment.Verdict != security.RiskDangerous && v.Assessment.Verdict != security.RiskUncertain {
			return errors.New("invalid risk verdict")
		}
		reason := v.Assessment.Reason
		if reason == "" || reason != strings.TrimSpace(reason) || len(reason) > 1000 || !utf8.ValidString(reason) {
			return errors.New("risk review reason is invalid")
		}
	default:
		return errors.New("unknown tool event kind")
	}
	return nil
}

func validToolEventKind(kind toolexecution.EventKind) bool {
	return kind == toolexecution.ApprovalRequired || kind == toolexecution.ApprovalDecided || kind == toolexecution.Reviewed
}
