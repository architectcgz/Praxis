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
	ID            AgentResultID
	SessionID     SessionID
	SourceAgentID AgentID
	Summary       string
	ChangedPaths  []string
	EvidenceRefs  []ContentRef
	Status        ArtifactReviewStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewAgentResult(
	id AgentResultID,
	sessionID SessionID,
	sourceAgentID AgentID,
	summary string,
	changedPaths []string,
	evidenceRefs []ContentRef,
	at time.Time,
) (AgentResult, error) {
	result := AgentResult{
		ID:            id,
		SessionID:     sessionID,
		SourceAgentID: sourceAgentID,
		Summary:       strings.TrimSpace(summary),
		ChangedPaths:  cloneStrings(changedPaths),
		EvidenceRefs:  cloneContentRefs(evidenceRefs),
		Status:        ArtifactDraft,
		CreatedAt:     at.UTC(),
		UpdatedAt:     at.UTC(),
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
	if idIsEmpty(string(r.ID)) || idIsEmpty(string(r.SessionID)) || idIsEmpty(string(r.SourceAgentID)) {
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
		r.SessionID,
		r.SourceAgentID,
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
		r.SessionID,
		r.SourceAgentID,
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
		r.SessionID,
		r.SourceAgentID,
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
	ID            BriefingID
	SessionID     SessionID
	SourceAgentID AgentID
	TargetAgentID AgentID
	Body          string
	Status        ArtifactReviewStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewBriefing(
	id BriefingID,
	sessionID SessionID,
	sourceAgentID, targetAgentID AgentID,
	body string,
	at time.Time,
) (Briefing, error) {
	briefing := Briefing{
		ID:            id,
		SessionID:     sessionID,
		SourceAgentID: sourceAgentID,
		TargetAgentID: targetAgentID,
		Body:          strings.TrimSpace(body),
		Status:        ArtifactDraft,
		CreatedAt:     at.UTC(),
		UpdatedAt:     at.UTC(),
	}
	if err := briefing.Validate(); err != nil {
		return Briefing{}, err
	}
	return briefing, nil
}

func (b Briefing) Validate() error {
	if idIsEmpty(string(b.ID)) || idIsEmpty(string(b.SessionID)) || idIsEmpty(string(b.SourceAgentID)) ||
		idIsEmpty(string(b.TargetAgentID)) {
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
		b.SessionID,
		b.SourceAgentID,
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
		b.SessionID,
		b.SourceAgentID,
		"briefing",
		b.ID.String(),
	)
	if err == nil {
		event.Payload["targetAgentId"] = b.TargetAgentID.String()
	}
	return event, err
}

func (b *Briefing) Reject(at time.Time) (DomainEvent, error) {
	return rejectArtifact(
		&b.Status,
		&b.UpdatedAt,
		b.Status,
		at,
		b.SessionID,
		b.SourceAgentID,
		"briefing",
		b.ID.String(),
	)
}

func (b Briefing) Snapshot() Briefing { return b }

func submitArtifact(
	status *ArtifactReviewStatus,
	updatedAt *time.Time,
	expected ArtifactReviewStatus,
	at time.Time,
	sessionID SessionID,
	sourceID AgentID,
	kind, id string,
) (DomainEvent, error) {
	if *status != expected {
		return DomainEvent{}, invalidTransition(kind, string(*status), string(ArtifactPendingApproval))
	}
	*status, *updatedAt = ArtifactPendingApproval, at.UTC()
	event := newDomainEvent(EventArtifactSubmitted, at)
	event.SessionID, event.AgentID = sessionID, sourceID
	event.Payload = map[string]string{"kind": kind, "id": id}
	return event, nil
}

func approveArtifact(
	status *ArtifactReviewStatus,
	updatedAt *time.Time,
	current ArtifactReviewStatus,
	at time.Time,
	sessionID SessionID,
	sourceID AgentID,
	kind, id string,
) (DomainEvent, error) {
	if current != ArtifactPendingApproval {
		return DomainEvent{}, invalidTransition(kind, string(current), string(ArtifactApproved))
	}
	*status, *updatedAt = ArtifactApproved, at.UTC()
	event := newDomainEvent(EventArtifactApproved, at)
	event.SessionID, event.AgentID = sessionID, sourceID
	event.Payload = map[string]string{"kind": kind, "id": id}
	return event, nil
}

func rejectArtifact(
	status *ArtifactReviewStatus,
	updatedAt *time.Time,
	current ArtifactReviewStatus,
	at time.Time,
	sessionID SessionID,
	sourceID AgentID,
	kind, id string,
) (DomainEvent, error) {
	if current != ArtifactPendingApproval {
		return DomainEvent{}, invalidTransition(kind, string(current), string(ArtifactRejected))
	}
	*status, *updatedAt = ArtifactRejected, at.UTC()
	event := newDomainEvent(EventArtifactRejected, at)
	event.SessionID, event.AgentID = sessionID, sourceID
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
