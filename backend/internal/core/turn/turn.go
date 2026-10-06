// Package turn 定义 loop 一次迭代的持久化身份、状态转换与结算不变量。
// 一次 Turn 对应一次 Provider 调用及其派生的工具调用；Task 是其归属的输入任务。
// 状态修改不提供并发同步；调用方必须在事务内读取、转换并保存，终态不得重新执行。
package turn

import (
	"fmt"
	"strings"
	"time"

	"praxis/internal/contracts"
)

// TurnStatus 表示本次迭代是否已结算。
type TurnStatus string

const (
	TurnRunning TurnStatus = "running"
	TurnEnded   TurnStatus = "ended"
)

// Valid 判断状态是否属于受支持的持久化状态集合。
func (s TurnStatus) Valid() bool {
	return s == TurnRunning || s == TurnEnded
}

// TurnOutcome 是已结算迭代的稳定结果分类。
type TurnOutcome string

const (
	TurnCompleted   TurnOutcome = "completed"
	TurnFailed      TurnOutcome = "failed"
	TurnInterrupted TurnOutcome = "interrupted"
)

// Valid 判断结果是否属于受支持的集合。
func (o TurnOutcome) Valid() bool {
	return o == TurnCompleted || o == TurnFailed || o == TurnInterrupted
}

// Turn 归属于一次 Task，以 Sequence 标记 loop 内的迭代次序。
// 身份由 NewTurnID 推导，因此同一迭代重复执行不会产生第二条记录。
type Turn struct {
	ID             contracts.TurnID
	TaskID         contracts.TaskID
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	Sequence       uint64
	Status         TurnStatus
	Outcome        TurnOutcome
	FailureCode    contracts.TaskFailureCode
	FailureMessage string
	CreatedAt      time.Time
	EndedAt        time.Time
}

// NewTurnID 由 Task 身份与迭代序号生成稳定标识，保证重复准入幂等。
func NewTurnID(taskID contracts.TaskID, sequence uint64) (contracts.TurnID, error) {
	if contracts.EmptyID(string(taskID)) || sequence == 0 {
		return "", contracts.InvalidValue("turn.id", "task and sequence are required")
	}
	return contracts.TurnID(fmt.Sprintf("turn:%s:%d", taskID, sequence)), nil
}

// NewRunning 创建 running 迭代，复制归属标识并把创建时间转换为 UTC。
func NewRunning(
	id contracts.TurnID,
	taskID contracts.TaskID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	sequence uint64,
	at time.Time,
) (Turn, error) {
	value := Turn{
		ID:        id,
		TaskID:    taskID,
		SessionID: sessionID,
		AgentID:   agentID,
		Sequence:  sequence,
		Status:    TurnRunning,
		CreatedAt: at.UTC(),
	}
	if err := value.Validate(); err != nil {
		return Turn{}, err
	}
	return value, nil
}

// Validate 只读校验身份、状态和时间顺序；持久化恢复必须调用。
// 非 canonical 字符串与互相矛盾的生命周期字段会被拒绝，不会被静默修正。
func (t Turn) Validate() error {
	for _, value := range []string{
		string(t.ID),
		string(t.TaskID),
		string(t.SessionID),
		string(t.AgentID),
	} {
		if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return contracts.InvalidValue("turn", "identity must be a non-empty canonical value")
		}
	}
	if !t.Status.Valid() || t.Sequence == 0 || t.CreatedAt.IsZero() {
		return contracts.InvalidValue("turn", "status, sequence, or creation time is invalid")
	}
	// 身份必须可由 Task 与序号重建，否则恢复时无法定位同一迭代。
	expected, err := NewTurnID(t.TaskID, t.Sequence)
	if err != nil || expected != t.ID {
		return contracts.InvalidValue("turn.id", "identity must be derived from task and sequence")
	}
	if t.FailureMessage != strings.TrimSpace(t.FailureMessage) ||
		strings.ContainsAny(t.FailureMessage, "\x00\r\n") ||
		len([]rune(t.FailureMessage)) > contracts.MaxTaskFailureMessageRunes {
		return contracts.InvalidValue("turn.failureMessage", "failure message must be canonical and bounded")
	}
	if !t.FailureCode.Valid() {
		return contracts.InvalidValue("turn.failureCode", "unknown failure code")
	}
	if t.Status == TurnRunning {
		if !t.EndedAt.IsZero() || t.Outcome != "" || t.FailureCode != "" || t.FailureMessage != "" {
			return contracts.InvalidValue("turn", "running iteration contains settlement fields")
		}
		return nil
	}
	if !t.Outcome.Valid() || t.EndedAt.IsZero() || t.EndedAt.Before(t.CreatedAt) {
		return contracts.InvalidValue("turn", "ended iteration requires an outcome and a settlement time")
	}
	switch t.Outcome {
	case TurnCompleted:
		if t.FailureCode != "" || t.FailureMessage != "" {
			return contracts.InvalidValue("turn", "completed iteration cannot contain a failure")
		}
	case TurnFailed:
		if t.FailureCode == "" {
			return contracts.InvalidValue("turn.failureCode", "failed iteration requires a failure code")
		}
	case TurnInterrupted:
		if t.FailureCode == "" || t.FailureMessage != "" {
			return contracts.InvalidValue("turn", "interrupted iteration requires a failure code and no message")
		}
	}
	return nil
}

// End 结算 running 迭代；重复结算或时间早于创建时间时拒绝修改。
func (t *Turn) End(outcome TurnOutcome, code contracts.TaskFailureCode, message string, at time.Time) error {
	if t.Status != TurnRunning {
		return contracts.InvalidTransition("turn", string(t.Status), string(TurnEnded))
	}
	next := *t
	next.Status = TurnEnded
	next.Outcome = outcome
	next.FailureCode = code
	next.FailureMessage = message
	next.EndedAt = at.UTC()
	if err := next.Validate(); err != nil {
		return err
	}
	*t = next
	return nil
}
