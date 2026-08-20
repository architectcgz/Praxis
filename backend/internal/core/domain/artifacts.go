package domain

import (
	"strings"
	"time"
)

type ArtifactReviewStatus string

const (
	ArtifactDraft           ArtifactReviewStatus = "draft"
	ArtifactPendingApproval ArtifactReviewStatus = "pending_approval"
	ArtifactApproved        ArtifactReviewStatus = "approved"
	ArtifactRejected        ArtifactReviewStatus = "rejected"
)

const (
	MaxArtifactSummaryBytes = 16 * 1024
	MaxBriefingBodyBytes    = 32 * 1024
	MaxChangedPaths         = 256
	MaxEvidenceRefs         = 128
)

type AgentResult struct {
	ID             AgentResultID
	TaskSessionID  TaskSessionID
	SourceThreadID AgentThreadID
	Summary        string
	ChangedPaths   []string
	EvidenceRefs   []ContentRef
	Status         ArtifactReviewStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewAgentResult(
	id AgentResultID,
	sessionID TaskSessionID,
	sourceThreadID AgentThreadID,
	summary string,
	changedPaths []string,
	evidenceRefs []ContentRef,
	at time.Time,
) (AgentResult, error) {
	result := AgentResult{
		ID:             id,
		TaskSessionID:  sessionID,
		SourceThreadID: sourceThreadID,
		Summary:        strings.TrimSpace(summary),
		ChangedPaths:   cloneStrings(changedPaths),
		EvidenceRefs:   cloneContentRefs(evidenceRefs),
		Status:         ArtifactDraft,
		CreatedAt:      at.UTC(),
		UpdatedAt:      at.UTC(),
	}
	for i := range result.ChangedPaths {
		result.ChangedPaths[i] = strings.TrimSpace(result.ChangedPaths[i])
	}
	if err := result.Validate(); err != nil {
		return AgentResult{}, err
	}
	return result, nil
}

func (r AgentResult) Validate() error {
	if idIsEmpty(string(r.ID)) || idIsEmpty(string(r.TaskSessionID)) || idIsEmpty(string(r.SourceThreadID)) {
		return invalidValue("agentResult", "required reference is missing")
	}
	if r.Summary == "" || len([]byte(r.Summary)) > MaxArtifactSummaryBytes {
		return invalidValue("agentResult.summary", "summary is empty or exceeds the bounded limit")
	}
	if len(r.ChangedPaths) > MaxChangedPaths || len(r.EvidenceRefs) > MaxEvidenceRefs {
		return invalidValue("agentResult", "too many result references")
	}
	for _, path := range r.ChangedPaths {
		if path == "" || strings.ContainsAny(path, "\x00\r\n") {
			return invalidValue("agentResult.changedPaths", "path is empty or contains a control character")
		}
	}
	for _, ref := range r.EvidenceRefs {
		if err := ref.Validate(); err != nil {
			return fmtField("agentResult.evidenceRefs", err)
		}
	}
	if !validArtifactStatus(r.Status) || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return invalidValue("agentResult", "invalid status or timestamp")
	}
	return nil
}

func (r *AgentResult) Submit(at time.Time) (DomainEvent, error) {
	return submitArtifact(
		&r.Status,
		&r.UpdatedAt,
		ArtifactDraft,
		at,
		r.TaskSessionID,
		r.SourceThreadID,
		"agent_result",
		r.ID.String(),
	)
}

func (r *AgentResult) Approve(at time.Time) (DomainEvent, error) {
	return approveArtifact(
		&r.Status,
		&r.UpdatedAt,
		r.Status,
		at,
		r.TaskSessionID,
		r.SourceThreadID,
		"agent_result",
		r.ID.String(),
	)
}

func (r *AgentResult) Reject(at time.Time) (DomainEvent, error) {
	return rejectArtifact(
		&r.Status,
		&r.UpdatedAt,
		r.Status,
		at,
		r.TaskSessionID,
		r.SourceThreadID,
		"agent_result",
		r.ID.String(),
	)
}

func (r AgentResult) Snapshot() AgentResult {
	copy := r
	copy.ChangedPaths = cloneStrings(r.ChangedPaths)
	copy.EvidenceRefs = cloneContentRefs(r.EvidenceRefs)
	return copy
}

type Briefing struct {
	ID             BriefingID
	TaskSessionID  TaskSessionID
	SourceThreadID AgentThreadID
	TargetThreadID AgentThreadID
	Body           string
	Status         ArtifactReviewStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewBriefing(
	id BriefingID,
	sessionID TaskSessionID,
	sourceThreadID, targetThreadID AgentThreadID,
	body string,
	at time.Time,
) (Briefing, error) {
	briefing := Briefing{
		ID:             id,
		TaskSessionID:  sessionID,
		SourceThreadID: sourceThreadID,
		TargetThreadID: targetThreadID,
		Body:           strings.TrimSpace(body),
		Status:         ArtifactDraft,
		CreatedAt:      at.UTC(),
		UpdatedAt:      at.UTC(),
	}
	if err := briefing.Validate(); err != nil {
		return Briefing{}, err
	}
	return briefing, nil
}

func (b Briefing) Validate() error {
	if idIsEmpty(string(b.ID)) || idIsEmpty(string(b.TaskSessionID)) || idIsEmpty(string(b.SourceThreadID)) ||
		idIsEmpty(string(b.TargetThreadID)) {
		return invalidValue("briefing", "required reference is missing")
	}
	if b.Body == "" || len([]byte(b.Body)) > MaxBriefingBodyBytes {
		return invalidValue("briefing.body", "body is empty or exceeds the bounded limit")
	}
	if !validArtifactStatus(b.Status) || b.CreatedAt.IsZero() || b.UpdatedAt.IsZero() {
		return invalidValue("briefing", "invalid status or timestamp")
	}
	return nil
}

func (b *Briefing) Submit(at time.Time) (DomainEvent, error) {
	return submitArtifact(
		&b.Status,
		&b.UpdatedAt,
		ArtifactDraft,
		at,
		b.TaskSessionID,
		b.SourceThreadID,
		"briefing",
		b.ID.String(),
	)
}

func (b *Briefing) Approve(at time.Time) (DomainEvent, error) {
	event, err := approveArtifact(
		&b.Status,
		&b.UpdatedAt,
		b.Status,
		at,
		b.TaskSessionID,
		b.SourceThreadID,
		"briefing",
		b.ID.String(),
	)
	if err == nil {
		event.Payload["targetThreadId"] = b.TargetThreadID.String()
	}
	return event, err
}

func (b *Briefing) Reject(at time.Time) (DomainEvent, error) {
	return rejectArtifact(
		&b.Status,
		&b.UpdatedAt,
		b.Status,
		at,
		b.TaskSessionID,
		b.SourceThreadID,
		"briefing",
		b.ID.String(),
	)
}

func (b Briefing) Snapshot() Briefing { return b }

type DeliveryStatus string

const (
	DeliveryPending    DeliveryStatus = "pending"
	DeliveryDelivering DeliveryStatus = "delivering"
	DeliveryDelivered  DeliveryStatus = "delivered"
	DeliveryFailed     DeliveryStatus = "failed"
)

type BriefingDelivery struct {
	ID              DeliveryID
	BriefingID      BriefingID
	TargetThreadID  AgentThreadID
	InjectionKey    string
	RuntimeEntryRef string
	Status          DeliveryStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
	FailureMessage  string
}

func NewBriefingDelivery(
	id DeliveryID,
	briefing Briefing,
	injectionKey string,
	at time.Time,
) (BriefingDelivery, error) {
	if briefing.Status != ArtifactApproved {
		return BriefingDelivery{}, invalidValue("briefingDelivery", "only an approved briefing can be delivered")
	}
	delivery := BriefingDelivery{
		ID:             id,
		BriefingID:     briefing.ID,
		TargetThreadID: briefing.TargetThreadID,
		InjectionKey:   strings.TrimSpace(injectionKey),
		Status:         DeliveryPending,
		CreatedAt:      at.UTC(),
		UpdatedAt:      at.UTC(),
	}
	if err := delivery.Validate(); err != nil {
		return BriefingDelivery{}, err
	}
	return delivery, nil
}

func (d BriefingDelivery) Validate() error {
	if idIsEmpty(string(d.ID)) || idIsEmpty(string(d.BriefingID)) || idIsEmpty(string(d.TargetThreadID)) ||
		d.InjectionKey == "" {
		return invalidValue("briefingDelivery", "required reference is missing")
	}
	if strings.ContainsAny(d.InjectionKey, "\x00\r\n") || !validDeliveryStatus(d.Status) || d.CreatedAt.IsZero() ||
		d.UpdatedAt.IsZero() {
		return invalidValue("briefingDelivery", "invalid key, status or timestamp")
	}
	return nil
}

func (d *BriefingDelivery) Begin(at time.Time) (DomainEvent, error) {
	if d.Status != DeliveryPending {
		return DomainEvent{}, invalidTransition("delivery", string(d.Status), string(DeliveryDelivering))
	}
	d.Status, d.UpdatedAt = DeliveryDelivering, at.UTC()
	event := newDomainEvent(EventDeliveryCreated, at)
	event.Delivery = d.ID
	return event, nil
}

func (d *BriefingDelivery) MarkDelivered(entryRef string, at time.Time) (DomainEvent, error) {
	if d.Status != DeliveryDelivering {
		return DomainEvent{}, invalidTransition("delivery", string(d.Status), string(DeliveryDelivered))
	}
	if strings.TrimSpace(entryRef) == "" {
		return DomainEvent{}, invalidValue("briefingDelivery.runtimeEntryRef", "runtime entry reference is required")
	}
	d.Status, d.RuntimeEntryRef, d.UpdatedAt = DeliveryDelivered, strings.TrimSpace(entryRef), at.UTC()
	event := newDomainEvent(EventDeliveryDelivered, at)
	event.Delivery = d.ID
	return event, nil
}

func (d *BriefingDelivery) Fail(message string, at time.Time) (DomainEvent, error) {
	if d.Status != DeliveryDelivering && d.Status != DeliveryPending {
		return DomainEvent{}, invalidTransition("delivery", string(d.Status), string(DeliveryFailed))
	}
	d.Status, d.FailureMessage, d.UpdatedAt = DeliveryFailed, strings.TrimSpace(message), at.UTC()
	event := newDomainEvent(EventDeliveryFailed, at)
	event.Delivery = d.ID
	return event, nil
}

func submitArtifact(
	status *ArtifactReviewStatus,
	updatedAt *time.Time,
	expected ArtifactReviewStatus,
	at time.Time,
	sessionID TaskSessionID,
	sourceID AgentThreadID,
	kind, id string,
) (DomainEvent, error) {
	if *status != expected {
		return DomainEvent{}, invalidTransition(kind, string(*status), string(ArtifactPendingApproval))
	}
	*status, *updatedAt = ArtifactPendingApproval, at.UTC()
	event := newDomainEvent(EventArtifactSubmitted, at)
	event.TaskSession, event.AgentThread = sessionID, sourceID
	event.Payload = map[string]string{"kind": kind, "id": id}
	return event, nil
}

func approveArtifact(
	status *ArtifactReviewStatus,
	updatedAt *time.Time,
	current ArtifactReviewStatus,
	at time.Time,
	sessionID TaskSessionID,
	sourceID AgentThreadID,
	kind, id string,
) (DomainEvent, error) {
	if current != ArtifactPendingApproval {
		return DomainEvent{}, invalidTransition(kind, string(current), string(ArtifactApproved))
	}
	*status, *updatedAt = ArtifactApproved, at.UTC()
	event := newDomainEvent(EventArtifactApproved, at)
	event.TaskSession, event.AgentThread = sessionID, sourceID
	event.Payload = map[string]string{"kind": kind, "id": id}
	return event, nil
}

func rejectArtifact(
	status *ArtifactReviewStatus,
	updatedAt *time.Time,
	current ArtifactReviewStatus,
	at time.Time,
	sessionID TaskSessionID,
	sourceID AgentThreadID,
	kind, id string,
) (DomainEvent, error) {
	if current != ArtifactPendingApproval {
		return DomainEvent{}, invalidTransition(kind, string(current), string(ArtifactRejected))
	}
	*status, *updatedAt = ArtifactRejected, at.UTC()
	event := newDomainEvent(EventArtifactRejected, at)
	event.TaskSession, event.AgentThread = sessionID, sourceID
	event.Payload = map[string]string{"kind": kind, "id": id}
	return event, nil
}

func validArtifactStatus(status ArtifactReviewStatus) bool {
	switch status {
	case ArtifactDraft, ArtifactPendingApproval, ArtifactApproved, ArtifactRejected:
		return true
	default:
		return false
	}
}

func validDeliveryStatus(status DeliveryStatus) bool {
	switch status {
	case DeliveryPending, DeliveryDelivering, DeliveryDelivered, DeliveryFailed:
		return true
	default:
		return false
	}
}
