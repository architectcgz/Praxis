package workflow

import (
	"praxis/internal/contracts"

	"strings"
	"time"
)

type WaitConditionKind string

const (
	WaitForDelivery WaitConditionKind = "delivery"
	WaitForApproval WaitConditionKind = "approval"
	WaitForChild    WaitConditionKind = "child"
)

type WaitConditionMode string

const (
	WaitAny WaitConditionMode = "any"
	WaitAll WaitConditionMode = "all"
)

type WaitConditionStatus string

const (
	WaitPending   WaitConditionStatus = "pending"
	WaitResolved  WaitConditionStatus = "resolved"
	WaitCancelled WaitConditionStatus = "cancelled"
)

type WaitCondition struct {
	ID                contracts.WaitConditionID
	AgentID           contracts.AgentID
	TurnID            contracts.TurnID
	Kind              WaitConditionKind
	Mode              WaitConditionMode
	TargetIDs         []string
	ResolvedTargetIDs []string
	Status            WaitConditionStatus
	CreatedAt         time.Time
	ResolvedAt        time.Time
}

func (w WaitCondition) Validate() error {
	if contracts.EmptyID(string(w.ID)) || contracts.EmptyID(string(w.AgentID)) || contracts.EmptyID(string(w.TurnID)) {
		return contracts.InvalidValue("waitCondition", "required reference is missing")
	}
	if !validWaitKind(w.Kind) || (w.Mode != WaitAny && w.Mode != WaitAll) || !validWaitStatus(w.Status) {
		return contracts.InvalidValue("waitCondition", "unknown kind, mode or status")
	}
	if len(w.TargetIDs) == 0 {
		return contracts.InvalidValue("waitCondition.targetIDs", "at least one target is required")
	}
	for _, targetID := range w.TargetIDs {
		if targetID == "" || targetID != strings.TrimSpace(targetID) || strings.ContainsAny(targetID, "\x00\r\n") {
			return contracts.InvalidValue("waitCondition.targetIDs", "target is invalid")
		}
	}
	seenTargets := make(map[string]struct{}, len(w.TargetIDs))
	for _, targetID := range w.TargetIDs {
		if _, duplicate := seenTargets[targetID]; duplicate {
			return contracts.InvalidValue("waitCondition.targetIDs", "duplicate target")
		}
		seenTargets[targetID] = struct{}{}
	}
	seenResolved := make(map[string]struct{}, len(w.ResolvedTargetIDs))
	for _, targetID := range w.ResolvedTargetIDs {
		if targetID == "" || targetID != strings.TrimSpace(targetID) || strings.ContainsAny(targetID, "\x00\r\n") {
			return contracts.InvalidValue("waitCondition.resolvedTargetIDs", "resolved target is invalid")
		}
		if _, known := seenTargets[targetID]; !known {
			return contracts.InvalidValue("waitCondition.resolvedTargetIDs", "resolved target is not awaited")
		}
		if _, duplicate := seenResolved[targetID]; duplicate {
			return contracts.InvalidValue("waitCondition.resolvedTargetIDs", "duplicate resolved target")
		}
		seenResolved[targetID] = struct{}{}
	}
	if w.CreatedAt.IsZero() || (!w.ResolvedAt.IsZero() && w.ResolvedAt.Before(w.CreatedAt)) {
		return contracts.InvalidValue("waitCondition.timestamps", "timestamps are invalid")
	}
	if w.Status == WaitPending && !w.ResolvedAt.IsZero() {
		return contracts.InvalidValue("waitCondition.resolvedAt", "pending wait has a resolution time")
	}
	if w.Status != WaitPending && w.ResolvedAt.IsZero() {
		return contracts.InvalidValue("waitCondition.resolvedAt", "final wait requires a resolution time")
	}
	if w.Status == WaitResolved {
		if w.Mode == WaitAny && len(w.ResolvedTargetIDs) == 0 {
			return contracts.InvalidValue("waitCondition.resolvedTargetIDs", "resolved any wait has no target")
		}
		if w.Mode == WaitAll && len(w.ResolvedTargetIDs) != len(w.TargetIDs) {
			return contracts.InvalidValue("waitCondition.resolvedTargetIDs", "resolved all wait has pending targets")
		}
	}
	return nil
}

func (w *WaitCondition) Cancel(at time.Time) error {
	if w.Status != WaitPending {
		return contracts.InvalidTransition("waitCondition", string(w.Status), string(WaitCancelled))
	}
	w.Status = WaitCancelled
	w.ResolvedAt = at.UTC()
	return nil
}

type AgentControlKind string

const (
	AgentControlPause  AgentControlKind = "pause"
	AgentControlCancel AgentControlKind = "cancel"
)

type AgentControlStatus string

const (
	AgentControlPending AgentControlStatus = "pending"
	AgentControlApplied AgentControlStatus = "applied"
)

type AgentControlCommand struct {
	ID           contracts.AgentControlCommandID
	AgentID      contracts.AgentID
	TargetTurnID contracts.TurnID
	Kind         AgentControlKind
	Status       AgentControlStatus
	CreatedAt    time.Time
	AppliedAt    time.Time
}

func NewAgentControlCommand(
	id contracts.AgentControlCommandID,
	agentID contracts.AgentID,
	targetTurnID contracts.TurnID,
	kind AgentControlKind,
	at time.Time,
) (AgentControlCommand, error) {
	request := AgentControlCommand{
		ID:           id,
		AgentID:      agentID,
		TargetTurnID: targetTurnID,
		Kind:         kind,
		Status:       AgentControlPending,
		CreatedAt:    at.UTC(),
	}
	if err := request.Validate(); err != nil {
		return AgentControlCommand{}, err
	}
	return request, nil
}

func (r AgentControlCommand) Validate() error {
	if contracts.EmptyID(string(r.ID)) || contracts.EmptyID(string(r.AgentID)) || contracts.EmptyID(string(r.TargetTurnID)) {
		return contracts.InvalidValue("agentControlCommand", "required reference is missing")
	}
	if r.Kind != AgentControlPause && r.Kind != AgentControlCancel {
		return contracts.InvalidValue("agentControlCommand.kind", "unknown control kind")
	}
	if r.Status != AgentControlPending && r.Status != AgentControlApplied {
		return contracts.InvalidValue("agentControlCommand.status", "unknown control status")
	}
	if r.CreatedAt.IsZero() || (!r.AppliedAt.IsZero() && r.AppliedAt.Before(r.CreatedAt)) {
		return contracts.InvalidValue("agentControlCommand.timestamps", "timestamps are invalid")
	}
	if r.Status == AgentControlPending && !r.AppliedAt.IsZero() {
		return contracts.InvalidValue("agentControlCommand.appliedAt", "pending control has an apply time")
	}
	if r.Status == AgentControlApplied && r.AppliedAt.IsZero() {
		return contracts.InvalidValue("agentControlCommand.appliedAt", "applied control requires a time")
	}
	return nil
}

func (r *AgentControlCommand) MarkApplied(at time.Time) error {
	if r.Status != AgentControlPending {
		return contracts.InvalidTransition("agentControlCommand", string(r.Status), string(AgentControlApplied))
	}
	r.Status = AgentControlApplied
	r.AppliedAt = at.UTC()
	return nil
}

func validWaitKind(kind WaitConditionKind) bool {
	return kind == WaitForDelivery || kind == WaitForApproval || kind == WaitForChild
}

func validWaitStatus(status WaitConditionStatus) bool {
	return status == WaitPending || status == WaitResolved || status == WaitCancelled
}
