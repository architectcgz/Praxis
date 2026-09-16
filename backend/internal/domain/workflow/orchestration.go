package workflow

import (
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
	ID                WaitConditionID
	AgentID           AgentID
	ExecutionID       AgentExecutionID
	Kind              WaitConditionKind
	Mode              WaitConditionMode
	TargetIDs         []string
	ResolvedTargetIDs []string
	Status            WaitConditionStatus
	CreatedAt         time.Time
	ResolvedAt        time.Time
}

func NewWaitCondition(
	id WaitConditionID,
	agentID AgentID,
	executionID AgentExecutionID,
	kind WaitConditionKind,
	mode WaitConditionMode,
	targetIDs []string,
	at time.Time,
) (WaitCondition, error) {
	condition := WaitCondition{
		ID:          id,
		AgentID:     agentID,
		ExecutionID: executionID,
		Kind:        kind,
		Mode:        mode,
		TargetIDs:   cloneStrings(targetIDs),
		Status:      WaitPending,
		CreatedAt:   at.UTC(),
	}
	for index := range condition.TargetIDs {
		condition.TargetIDs[index] = strings.TrimSpace(condition.TargetIDs[index])
	}
	if err := condition.Validate(); err != nil {
		return WaitCondition{}, err
	}
	return condition, nil
}

func (w WaitCondition) Validate() error {
	if idIsEmpty(string(w.ID)) || idIsEmpty(string(w.AgentID)) || idIsEmpty(string(w.ExecutionID)) {
		return invalidValue("waitCondition", "required reference is missing")
	}
	if !validWaitKind(w.Kind) || (w.Mode != WaitAny && w.Mode != WaitAll) || !validWaitStatus(w.Status) {
		return invalidValue("waitCondition", "unknown kind, mode or status")
	}
	if len(w.TargetIDs) == 0 {
		return invalidValue("waitCondition.targetIDs", "at least one target is required")
	}
	for _, targetID := range w.TargetIDs {
		if targetID == "" || strings.ContainsAny(targetID, "\x00\r\n") {
			return invalidValue("waitCondition.targetIDs", "target is invalid")
		}
	}
	seenTargets := make(map[string]struct{}, len(w.TargetIDs))
	for _, targetID := range w.TargetIDs {
		seenTargets[targetID] = struct{}{}
	}
	seenResolved := make(map[string]struct{}, len(w.ResolvedTargetIDs))
	for _, targetID := range w.ResolvedTargetIDs {
		if _, known := seenTargets[targetID]; !known {
			return invalidValue("waitCondition.resolvedTargetIDs", "resolved target is not awaited")
		}
		if _, duplicate := seenResolved[targetID]; duplicate {
			return invalidValue("waitCondition.resolvedTargetIDs", "duplicate resolved target")
		}
		seenResolved[targetID] = struct{}{}
	}
	if w.CreatedAt.IsZero() || (!w.ResolvedAt.IsZero() && w.ResolvedAt.Before(w.CreatedAt)) {
		return invalidValue("waitCondition.timestamps", "timestamps are invalid")
	}
	if w.Status == WaitPending && !w.ResolvedAt.IsZero() {
		return invalidValue("waitCondition.resolvedAt", "pending wait has a resolution time")
	}
	if w.Status != WaitPending && w.ResolvedAt.IsZero() {
		return invalidValue("waitCondition.resolvedAt", "final wait requires a resolution time")
	}
	if w.Status == WaitResolved {
		if w.Mode == WaitAny && len(w.ResolvedTargetIDs) == 0 {
			return invalidValue("waitCondition.resolvedTargetIDs", "resolved any wait has no target")
		}
		if w.Mode == WaitAll && len(w.ResolvedTargetIDs) != len(w.TargetIDs) {
			return invalidValue("waitCondition.resolvedTargetIDs", "resolved all wait has pending targets")
		}
	}
	return nil
}

func (w *WaitCondition) Resolve(at time.Time) error {
	if w.Status != WaitPending {
		return invalidTransition("waitCondition", string(w.Status), string(WaitResolved))
	}
	w.ResolvedTargetIDs = cloneStrings(w.TargetIDs)
	w.Status = WaitResolved
	w.ResolvedAt = at.UTC()
	return nil
}

// ResolveTarget records one eligible durable target. A wait resolves when its
// any/all mode has been satisfied; unrelated delivery does not affect it.
func (w *WaitCondition) ResolveTarget(targetID string, at time.Time) (bool, error) {
	if w.Status != WaitPending {
		return false, invalidTransition("waitCondition", string(w.Status), string(WaitResolved))
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" || !containsString(w.TargetIDs, targetID) {
		return false, invalidValue("waitCondition.targetID", "target is not awaited")
	}
	if containsString(w.ResolvedTargetIDs, targetID) {
		return false, nil
	}
	w.ResolvedTargetIDs = append(w.ResolvedTargetIDs, targetID)
	if w.Mode == WaitAny || len(w.ResolvedTargetIDs) == len(w.TargetIDs) {
		w.Status = WaitResolved
		w.ResolvedAt = at.UTC()
		return true, nil
	}
	return false, nil
}

func (w *WaitCondition) Cancel(at time.Time) error {
	if w.Status != WaitPending {
		return invalidTransition("waitCondition", string(w.Status), string(WaitCancelled))
	}
	w.Status = WaitCancelled
	w.ResolvedAt = at.UTC()
	return nil
}

type AgentControlKind string

const (
	AgentControlPause AgentControlKind = "pause"
	AgentControlClose AgentControlKind = "close"
)

type AgentControlStatus string

const (
	AgentControlPending AgentControlStatus = "pending"
	AgentControlApplied AgentControlStatus = "applied"
)

type AgentControlCommand struct {
	ID                AgentControlCommandID
	AgentID           AgentID
	TargetExecutionID AgentExecutionID
	Kind              AgentControlKind
	Status            AgentControlStatus
	CreatedAt         time.Time
	AppliedAt         time.Time
}

func NewAgentControlCommand(
	id AgentControlCommandID,
	agentID AgentID,
	targetExecutionID AgentExecutionID,
	kind AgentControlKind,
	at time.Time,
) (AgentControlCommand, error) {
	request := AgentControlCommand{
		ID:                id,
		AgentID:           agentID,
		TargetExecutionID: targetExecutionID,
		Kind:              kind,
		Status:            AgentControlPending,
		CreatedAt:         at.UTC(),
	}
	if err := request.Validate(); err != nil {
		return AgentControlCommand{}, err
	}
	return request, nil
}

func (r AgentControlCommand) Validate() error {
	if idIsEmpty(string(r.ID)) || idIsEmpty(string(r.AgentID)) {
		return invalidValue("agentControlCommand", "required reference is missing")
	}
	if r.Kind != AgentControlPause && r.Kind != AgentControlClose {
		return invalidValue("agentControlCommand.kind", "unknown control kind")
	}
	if r.Status != AgentControlPending && r.Status != AgentControlApplied {
		return invalidValue("agentControlCommand.status", "unknown control status")
	}
	if r.CreatedAt.IsZero() || (!r.AppliedAt.IsZero() && r.AppliedAt.Before(r.CreatedAt)) {
		return invalidValue("agentControlCommand.timestamps", "timestamps are invalid")
	}
	if r.Status == AgentControlPending && !r.AppliedAt.IsZero() {
		return invalidValue("agentControlCommand.appliedAt", "pending control has an apply time")
	}
	if r.Status == AgentControlApplied && r.AppliedAt.IsZero() {
		return invalidValue("agentControlCommand.appliedAt", "applied control requires a time")
	}
	return nil
}

func (r *AgentControlCommand) MarkApplied(at time.Time) error {
	if r.Status != AgentControlPending {
		return invalidTransition("agentControlCommand", string(r.Status), string(AgentControlApplied))
	}
	r.Status = AgentControlApplied
	r.AppliedAt = at.UTC()
	return nil
}

type ContextDeliveryStatus string

const (
	ContextDeliveryPending    ContextDeliveryStatus = "pending"
	ContextDeliveryDelivering ContextDeliveryStatus = "delivering"
	ContextDeliveryDelivered  ContextDeliveryStatus = "delivered"
	ContextDeliveryRejected   ContextDeliveryStatus = "rejected"
	ContextDeliveryCancelled  ContextDeliveryStatus = "cancelled"
	ContextDeliveryFailed     ContextDeliveryStatus = "failed"
)

// ContextDelivery references an approved, structured artifact. It never
// carries a source Agent's transcript or acts as a normal user input mailbox.
type ContextDelivery struct {
	ID                DeliveryID
	SessionID         SessionID
	SourceArtifactID  string
	TargetAgentID     AgentID
	DedupeKey         string
	ArtifactEntryRef  string
	ResultExecutionID AgentExecutionID
	Status            ContextDeliveryStatus
	FailureCode       string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func NewContextDelivery(
	id DeliveryID,
	sessionID SessionID,
	sourceArtifactID string,
	targetAgentID AgentID,
	dedupeKey string,
	at time.Time,
) (ContextDelivery, error) {
	delivery := ContextDelivery{
		ID:               id,
		SessionID:        sessionID,
		SourceArtifactID: strings.TrimSpace(sourceArtifactID),
		TargetAgentID:    targetAgentID,
		DedupeKey:        strings.TrimSpace(dedupeKey),
		Status:           ContextDeliveryPending,
		CreatedAt:        at.UTC(),
		UpdatedAt:        at.UTC(),
	}
	if err := delivery.Validate(); err != nil {
		return ContextDelivery{}, err
	}
	return delivery, nil
}

func (d ContextDelivery) Validate() error {
	if idIsEmpty(string(d.ID)) || idIsEmpty(string(d.SessionID)) || d.SourceArtifactID == "" ||
		idIsEmpty(string(d.TargetAgentID)) || d.DedupeKey == "" {
		return invalidValue("contextDelivery", "required reference is missing")
	}
	if !validContextDeliveryStatus(d.Status) || strings.ContainsAny(d.DedupeKey, "\x00\r\n") {
		return invalidValue("contextDelivery", "invalid status or dedupe key")
	}
	if d.CreatedAt.IsZero() || d.UpdatedAt.IsZero() || d.UpdatedAt.Before(d.CreatedAt) {
		return invalidValue("contextDelivery.timestamps", "timestamps are invalid")
	}
	if d.Status == ContextDeliveryDelivered && (d.ArtifactEntryRef == "" || d.ResultExecutionID == "") {
		return invalidValue("contextDelivery.artifactEntryRef", "delivered entry reference and execution are required")
	}
	return nil
}

func (d *ContextDelivery) Begin(at time.Time) error {
	if d.Status != ContextDeliveryPending {
		return invalidTransition("contextDelivery", string(d.Status), string(ContextDeliveryDelivering))
	}
	d.Status = ContextDeliveryDelivering
	d.UpdatedAt = at.UTC()
	return nil
}

func (d *ContextDelivery) MarkDelivered(entryRef string, executionID AgentExecutionID, at time.Time) error {
	if d.Status != ContextDeliveryDelivering {
		return invalidTransition("contextDelivery", string(d.Status), string(ContextDeliveryDelivered))
	}
	if strings.TrimSpace(entryRef) == "" || idIsEmpty(string(executionID)) {
		return invalidValue("contextDelivery.artifactEntryRef", "entry reference and execution are required")
	}
	d.Status = ContextDeliveryDelivered
	d.ArtifactEntryRef = strings.TrimSpace(entryRef)
	d.ResultExecutionID = executionID
	d.UpdatedAt = at.UTC()
	return nil
}

func (d *ContextDelivery) ReturnPending(at time.Time) error {
	if d.Status != ContextDeliveryDelivering {
		return invalidTransition("contextDelivery", string(d.Status), string(ContextDeliveryPending))
	}
	d.Status = ContextDeliveryPending
	d.UpdatedAt = at.UTC()
	return nil
}

func validWaitKind(kind WaitConditionKind) bool {
	return kind == WaitForDelivery || kind == WaitForApproval || kind == WaitForChild
}

func validWaitStatus(status WaitConditionStatus) bool {
	return status == WaitPending || status == WaitResolved || status == WaitCancelled
}

func validContextDeliveryStatus(status ContextDeliveryStatus) bool {
	switch status {
	case ContextDeliveryPending, ContextDeliveryDelivering, ContextDeliveryDelivered,
		ContextDeliveryRejected, ContextDeliveryCancelled, ContextDeliveryFailed:
		return true
	default:
		return false
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
