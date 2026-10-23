package storage

import (
	"errors"
	"praxis/internal/core/security"
	"praxis/internal/core/toolexecution"
	"strings"
	"unicode/utf8"
)

func validToolEventKind(kind toolexecution.EventKind) bool {
	switch kind {
	case toolexecution.ApprovalRequired, toolexecution.Approved, toolexecution.Started, toolexecution.StopRequested, toolexecution.Reviewed:
		return true
	}
	return false
}

// validateToolEvent 只在磁盘恢复边界校验当前控制事件格式。
func validateToolEvent(e toolexecution.Event) error {
	for _, value := range []string{e.ExecutionID.String(), e.SessionID.String(), e.CallID} {
		if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("tool event identity must be canonical and nonempty")
		}
	}
	if !validToolEventKind(e.Kind) || e.At.IsZero() {
		return errors.New("tool event kind and time are required")
	}
	switch e.Kind {
	case toolexecution.ApprovalRequired, toolexecution.Approved:
		if e.Summary == "" || !e.Decision.Action.Valid() || e.Decision.RuleID == "" {
			return errors.New("approval event must include the arguments and rule")
		}
		if e.Kind == toolexecution.Approved {
			switch e.Source {
			case toolexecution.ApprovalSourceUser, toolexecution.ApprovalSourceYolo, toolexecution.ApprovalSourceAuto:
			default:
				return errors.New("unknown approval source")
			}
		} else if e.Source != "" {
			return errors.New("approval request cannot contain an approval source")
		}
	case toolexecution.Reviewed:
		if e.Review == nil {
			return errors.New("risk review evidence is required")
		}
		r := e.Review
		if r.TurnID == "" || r.ProviderID == "" || r.ModelID == "" || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || !e.At.Equal(r.CompletedAt) {
			return errors.New("risk review identity and times are invalid")
		}
		if r.Assessment.Verdict != security.RiskSafe && r.Assessment.Verdict != security.RiskDangerous && r.Assessment.Verdict != security.RiskUncertain {
			return errors.New("invalid risk verdict")
		}
		reason := r.Assessment.Reason
		if reason == "" || reason != strings.TrimSpace(reason) || len(reason) > 1000 || !utf8.ValidString(reason) {
			return errors.New("risk review reason is invalid")
		}
	}
	if e.Kind != toolexecution.Reviewed && e.Review != nil {
		return errors.New("only reviewed events contain risk evidence")
	}
	return nil
}
