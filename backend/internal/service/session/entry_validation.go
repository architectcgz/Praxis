package session

import (
	"errors"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func validateEntry(owner *sessionmodel.Session, data sessionentry.ContextEntry) error {
	current := owner.Branch(data.BranchID)
	if current == nil || current.HeadEntryID != data.ParentID {
		return errorcode.ErrRequestConflict
	}
	if err := data.ValidateContent(); err != nil {
		return err
	}
	if data.Role == role.Assistant && (data.AuthorKind != sessionentry.AuthorAgent || data.AuthorID != owner.MainAgentID().String()) {
		return errors.New("assistant author does not match the session main agent")
	}
	if data.ExecutionID == "" {
		return errors.New("execution ownership is required")
	}
	index := owner.ExecutionIndex(identity.AgentExecutionID(data.ExecutionID))
	preparing := index < 0 && data.Role == role.User && !data.IsSummary() && data.ID == "input:"+data.RequestID
	if !preparing {
		if index < 0 {
			return errorcode.ErrNotFound
		}
		execution := owner.Executions[index]
		if execution.BranchID != data.BranchID || execution.AgentID != owner.MainAgentID() || execution.RequestID.String() != data.RequestID {
			return errorcode.ErrRequestConflict
		}
	}
	if data.IsSummary() {
		if data.AuthorID != owner.MainAgentID().String() {
			return errors.New("summary author does not match the session main agent")
		}
		last, found := owner.Entry(data.ParentID)
		if !found || last.ExecutionID != data.ExecutionID || last.Role != role.Assistant || last.StopReason != "completed" && last.StopReason != "stop_sequence" {
			return errors.New("summary requires a completed user round")
		}
		return nil
	}
	needsPath := false
	for _, block := range data.Content {
		if block.Kind == sessionentry.BlockToolCall || block.Kind == sessionentry.BlockToolResult {
			needsPath = true
			break
		}
	}
	if !needsPath {
		return nil
	}
	entries := owner.Path(current.HeadEntryID)
	for _, block := range data.Content {
		if block.Kind != sessionentry.BlockToolCall && block.Kind != sessionentry.BlockToolResult {
			continue
		}
		var declaration *sessionentry.ContextEntry
		var declaredName string
		for _, entry := range entries {
			if entry.ExecutionID != data.ExecutionID {
				continue
			}
			for _, prior := range entry.Content {
				if prior.CallID != block.CallID {
					continue
				}
				if prior.Kind == sessionentry.BlockToolCall {
					if block.Kind == sessionentry.BlockToolCall && entry.ID != data.ID {
						return errorcode.ErrRequestConflict
					}
					declaration, declaredName = entry, prior.Name
				}
				if block.Kind == sessionentry.BlockToolResult && prior.Kind == sessionentry.BlockToolResult && entry.ID != data.ID {
					return errorcode.ErrRequestConflict
				}
			}
		}
		if block.Kind == sessionentry.BlockToolResult {
			if declaration == nil {
				return fmt.Errorf("tool call %q: %w", block.CallID, errorcode.ErrNotFound)
			}
			if declaration.RequestID != data.RequestID || declaration.TurnID != data.TurnID || declaredName != block.Name {
				return errors.New("tool receipt ownership does not match its declaration")
			}
		}
	}
	return nil
}
