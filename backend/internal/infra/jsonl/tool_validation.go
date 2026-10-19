package jsonl

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/utils/pathutil"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var storedToolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

// validateToolExecution 只在磁盘输入边界检查调用快照，保留审批绑定和生命周期约束。
func validateToolExecution(i toolexecution.Execution) error {
	for _, value := range []string{
		i.ID.String(),
		i.ExecutionID.String(),
		i.TurnID.String(),
		i.SessionID.String(),
		i.AgentID.String(),
		i.ProviderToolCallID,
		i.ArgumentsDigest,
	} {
		if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return fault.InvalidValue("toolExecution", "identity and digest must be non-empty canonical values")
		}
	}
	if !storedToolNamePattern.MatchString(string(i.Name)) || len(i.Arguments) == 0 || !json.Valid(i.Arguments) {
		return fault.InvalidValue("toolExecution", "tool name and valid JSON arguments are required")
	}
	if i.ArgumentsDigest != toolexecution.ArgumentsDigest(i.Name, i.Arguments) {
		return fault.InvalidValue("toolExecution.argumentsDigest", "arguments digest does not match the original call")
	}
	if i.Path != "" && !pathutil.IsAbsoluteNormalized(i.Path) {
		return fault.InvalidValue("toolExecution.path", "authorized path must be canonical")
	}
	if i.Name == tool.ToolBash && len(i.NormalizedArguments) > 0 {
		if err := validateStoredBashArguments(i.NormalizedArguments, i.Path); err != nil {
			return err
		}
	} else if len(i.NormalizedArguments) > 0 && !json.Valid(i.NormalizedArguments) {
		return fault.InvalidValue("toolExecution.arguments", "normalized arguments must be valid JSON")
	}
	if err := validateStoredAuthorization(i); err != nil {
		return err
	}
	if !i.ApprovedAt.IsZero() && len(i.NormalizedArguments) == 0 {
		return fault.InvalidValue("toolExecution", "approved invocation requires normalized arguments")
	}
	if !i.Status.Valid() || !i.FailureCode.Valid() || i.CreatedAt.IsZero() {
		return fault.InvalidValue("toolExecution", "status, failure code, or creation time is invalid")
	}
	if len(i.Result.InlineContent) > tool.MaxInlineResultBytes || !i.Result.ErrorCode.Valid() {
		return fault.InvalidValue("toolExecution.result", "result size or failure code is invalid")
	}
	if i.ApprovedAt.IsZero() {
		if i.Approval != (toolexecution.ApprovalRecord{}) || !i.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution.approval", "unapproved invocation has approval or start fields")
		}
	} else {
		switch i.Approval.Source {
		case toolexecution.ApprovalSourceUser, toolexecution.ApprovalSourceYolo, toolexecution.ApprovalSourceAuto:
		default:
			return fault.InvalidValue("toolExecution.approval.source", "unknown approval source")
		}
		if !i.Approval.ApprovedAt.Equal(i.ApprovedAt) || i.Approval.ArgumentsDigest != toolexecution.ArgumentsDigest(i.Name, i.NormalizedArguments) {
			return fault.InvalidValue("toolExecution.approval", "approval time or arguments digest is inconsistent")
		}
	}
	previous := i.CreatedAt
	for _, at := range []time.Time{i.ApprovedAt, i.StartedAt, i.SettledAt} {
		if !at.IsZero() {
			if at.Before(previous) {
				return fault.InvalidValue("toolExecution.timestamps", "lifecycle timestamps are out of order")
			}
			previous = at
		}
	}
	if i.Status.Terminal() {
		if i.SettledAt.IsZero() || i.Result.ErrorCode != i.FailureCode {
			return fault.InvalidValue("toolExecution", "terminal invocation requires a settlement time and matching result code")
		}
	} else if !i.SettledAt.IsZero() || i.FailureCode != toolexecution.FailureNone || i.Result != (toolexecution.Result{}) {
		return fault.InvalidValue("toolExecution", "active invocation contains terminal fields")
	}
	if i.StartedAt.IsZero() && i.Result.SideEffect {
		return fault.InvalidValue("toolExecution.result", "unstarted invocation cannot report a side effect")
	}
	switch i.Status {
	case toolexecution.StatusRequested, toolexecution.StatusPrepared, toolexecution.StatusReviewing, toolexecution.StatusAwaitingApproval, toolexecution.StatusDenied:
		if !i.ApprovedAt.IsZero() {
			return fault.InvalidValue("toolExecution.approvedAt", "unapproved invocation has an approval time")
		}
	case toolexecution.StatusApproved:
		if i.ApprovedAt.IsZero() || !i.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution", "approved invocation has invalid lifecycle fields")
		}
	case toolexecution.StatusRunning, toolexecution.StatusSucceeded, toolexecution.StatusFailed, toolexecution.StatusUnknown:
		if i.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution.startedAt", "executed invocation requires a start time")
		}
	}
	switch i.Status {
	case toolexecution.StatusSucceeded:
		if i.FailureCode != toolexecution.FailureNone {
			return fault.InvalidValue("toolExecution.result", "successful invocation cannot contain a failure")
		}
	case toolexecution.StatusFailed:
		if i.FailureCode == toolexecution.FailureNone {
			return fault.InvalidValue("toolExecution.result", "failed invocation requires a failure code")
		}
	case toolexecution.StatusDenied:
		if i.FailureCode != toolexecution.FailureResourceLimit && i.FailureCode != toolexecution.FailureNotAllowed && i.FailureCode != toolexecution.FailureApprovalDenied && i.FailureCode != toolexecution.FailureRiskDenied && i.FailureCode != toolexecution.FailureInvalidCall {
			return fault.InvalidValue("toolExecution.result", "denial requires a denial failure code")
		}
	case toolexecution.StatusInterrupted:
		if i.FailureCode != toolexecution.FailureInterrupted {
			return fault.InvalidValue("toolExecution.result", "interruption requires its stable failure code")
		}
	case toolexecution.StatusUnknown:
		if i.FailureCode != toolexecution.FailureResultUnknown {
			return fault.InvalidValue("toolExecution.result", "unknown result requires its stable failure code")
		}
	}
	return nil
}

// validateStoredBashArguments 在恢复入口读取冻结参数，不清洗字段或补默认值。
// 参数内目录必须与持久化授权路径相同，避免改变已审批对象。
func validateStoredBashArguments(data json.RawMessage, path string) error {
	var command, directory string
	var timeout float64
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return errors.New("stored bash arguments must be an object")
	}
	fields := map[string]any{
		"command": &command,
		"path":    &directory,
		"timeout": &timeout,
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		field, ok := token.(string)
		if !ok {
			return errors.New("stored bash argument name is invalid")
		}
		target, ok := fields[field]
		if !ok {
			return errors.New("stored bash arguments contain an unknown or duplicate field")
		}
		// 字段解码后移除，使重复字段与未知字段一并被拒绝。
		delete(fields, field)
		if err := decoder.Decode(target); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON data")
	}
	if strings.TrimSpace(command) == "" || len(command) > 128*1024 || directory == "" || directory != path || timeout <= 0 || timeout > 600 {
		return errors.New("stored bash arguments are invalid")
	}
	return nil
}

func validateStoredAuthorization(i toolexecution.Execution) error {
	if (i.Status == toolexecution.StatusPrepared || i.Status == toolexecution.StatusReviewing) && len(i.NormalizedArguments) == 0 {
		return fault.InvalidValue("toolExecution.arguments", "prepared operation requires frozen arguments")
	}
	if i.Decision != (security.ApprovalDecision{}) && (!i.Decision.Action.Valid() || i.Decision.RuleID == "") {
		return fault.InvalidValue("toolExecution.decision", "a valid action and rule identity are required")
	}
	for index, review := range i.Reviews {
		if review.TurnID == "" || review.ArgumentsDigest != toolexecution.ArgumentsDigest(i.Name, i.NormalizedArguments) || review.ProviderID == "" || review.ModelID == "" || review.StartedAt.Before(i.CreatedAt) || review.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution.review", "risk review must match the frozen operation and model identity")
		}
		if review.CompletedAt.IsZero() {
			if index != len(i.Reviews)-1 || i.Status != toolexecution.StatusReviewing && i.Status != toolexecution.StatusInterrupted || review.Assessment != (security.RiskAssessment{}) {
				return fault.InvalidValue("toolExecution.review", "only an active or interrupted review may be incomplete")
			}
		} else {
			if review.CompletedAt.Before(review.StartedAt) {
				return fault.InvalidValue("toolExecution.review", "risk review completion precedes its start")
			}
			if err := validateStoredAssessment(review.Assessment); err != nil {
				return err
			}
		}
	}
	if i.Status == toolexecution.StatusReviewing && (len(i.Reviews) == 0 || !i.LatestReview().CompletedAt.IsZero() || i.Decision.Action != security.ApprovalAuto) {
		return fault.InvalidValue("toolExecution.review", "reviewing operation requires a pending auto assessment")
	}
	if i.Approval.Source == toolexecution.ApprovalSourceAuto && (i.LatestReview().Assessment.Verdict != security.RiskSafe || i.Decision.Action != security.ApprovalAuto) {
		return fault.InvalidValue("toolExecution.approval", "auto approval requires an auto decision and durable safe assessment")
	}
	if i.Decision != (security.ApprovalDecision{}) {
		if i.Approval.Source == toolexecution.ApprovalSourceYolo && i.Decision.Action != security.ApprovalYolo {
			return fault.InvalidValue("toolExecution.approval", "yolo approval cannot satisfy a different action")
		}
		if i.Approval.Source == toolexecution.ApprovalSourceUser && i.Decision.Action == security.ApprovalAuto && i.LatestReview().Assessment.Verdict != security.RiskUncertain {
			return fault.InvalidValue("toolExecution.approval", "manual approval of an auto operation requires an uncertain assessment")
		}
	}
	return nil
}

func validateStoredAssessment(a security.RiskAssessment) error {
	if a.Verdict != security.RiskSafe && a.Verdict != security.RiskDangerous && a.Verdict != security.RiskUncertain {
		return fault.InvalidValue("toolExecution.review", "risk assessment has an invalid verdict")
	}
	if a.Reason == "" || a.Reason != strings.TrimSpace(a.Reason) || len(a.Reason) > 1000 || !utf8.ValidString(a.Reason) {
		return fault.InvalidValue("toolExecution.review", "risk assessment requires a canonical reason within 1000 bytes")
	}
	return nil
}
