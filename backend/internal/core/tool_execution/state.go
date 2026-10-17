package toolexecution

import (
	"bytes"
	"errors"

	"praxis/internal/contracts"
	"praxis/internal/core/session"
)

// ApplyRecord 根据一条执行记录更新工具状态；失败时不修改输入映射。
// Started 且没有 ToolResult 的调用恢复为 unknown；进程内是否仍在运行由执行层判断。
func ApplyRecord(states map[CallKey]State, event Record) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if event.Type == RecordMessage && event.Message.Role != session.RoleTool {
		updates := make(map[CallKey]State)
		callIndex := 0
		for _, block := range event.Message.Blocks {
			if block.Kind != session.BlockToolCall {
				continue
			}
			key := event.Key()
			key.CallID = block.CallID
			if err := key.Validate(); err != nil {
				return err
			}
			name := contracts.ToolName(block.Name)
			if !name.Valid() || len(block.Input) == 0 || !canonicalID(event.Message.TurnID) {
				return errors.New("tool declaration requires a registered name, arguments and turn identity")
			}
			if _, exists := states[key]; exists {
				return contracts.ErrRequestConflict
			}
			if _, exists := updates[key]; exists {
				return contracts.ErrRequestConflict
			}
			updates[key] = State{
				Key:              key,
				Name:             name,
				TurnID:           contracts.TurnID(event.Message.TurnID),
				Arguments:        bytes.Clone(block.Input),
				Status:           StatusNotStarted,
				Sequence:         event.Sequence,
				DeclaredSequence: event.Sequence,
				CallIndex:        callIndex,
			}
			callIndex++
		}
		for key, state := range updates {
			states[key] = state
		}
		return nil
	}
	key := event.Key()
	current, ok := states[key]
	if !ok {
		return contracts.ErrNotFound
	}
	if event.Sequence <= current.Sequence || current.ResultMessageID != "" {
		return contracts.ErrRequestConflict
	}
	if event.Type == RecordMessage {
		block := event.Message.Blocks[0]
		if block.Name != string(current.Name) || event.Message.TurnID != current.TurnID.String() ||
			event.Outcome.SideEffect && current.StartedAt.IsZero() {
			return errors.New("tool result does not match its declaration or execution history")
		}
		switch event.Outcome.Status {
		case StatusSucceeded, StatusFailed, StatusUnknown:
			if current.StartedAt.IsZero() {
				return errors.New("executed tool result requires a committed started event")
			}
		case StatusDenied:
			if !current.StartedAt.IsZero() {
				return errors.New("started tool cannot be denied retroactively")
			}
		}
		outcome := *event.Outcome
		current.Outcome = &outcome
		current.Status = outcome.Status
		current.ResultMessageID = event.Message.ID
		current.OutputBytes = int64(len(block.Text))
	} else {
		if !current.StartedAt.IsZero() {
			return contracts.ErrRequestConflict
		}
		switch event.Type {
		case RecordReserved:
			if current.Reserved {
				return contracts.ErrRequestConflict
			}
			current.Reserved = true
		case RecordPrepared:
			if !current.Reserved || current.Prepared != nil {
				return errors.New("preparation requires a reserved, unprepared tool call")
			}
			prepared := *event.Prepared
			prepared.NormalizedArguments = bytes.Clone(prepared.NormalizedArguments)
			current.Prepared = &prepared
			current.Status = StatusPrepared
		case RecordApprovalRequested:
			if current.Prepared == nil || current.Status == StatusAwaitingApproval {
				return errors.New("approval request requires a prepared tool call")
			}
			current.Approval = nil
			current.Status = StatusAwaitingApproval
		case RecordApproved:
			if current.Prepared == nil || current.Approval != nil ||
				current.Status == StatusAwaitingApproval && event.Approval.Source != contracts.ApprovalSourceUser {
				return errors.New("approval does not match the current review state")
			}
			if event.Approval.ArgumentsDigest != ArgumentsDigest(current.Name, current.Prepared.NormalizedArguments) {
				return errors.New("approval digest does not match the frozen tool arguments")
			}
			approval := *event.Approval
			current.Approval = &approval
			current.Status = StatusApproved
		case RecordStarted:
			if !current.Reserved || current.Prepared == nil || current.Approval == nil || current.Status != StatusApproved {
				return errors.New("tool start requires a reservation, frozen arguments and valid approval")
			}
			current.StartedAt = event.CreatedAt
			current.Status = StatusUnknown
		}
	}
	current.Sequence = event.Sequence
	states[key] = current
	return nil
}
