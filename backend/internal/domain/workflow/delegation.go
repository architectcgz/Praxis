package workflow

import "time"

type DelegationStatus string

const (
	DelegationDraft           DelegationStatus = "draft"
	DelegationPendingApproval DelegationStatus = "pending_approval"
	DelegationApproved        DelegationStatus = "approved"
	DelegationRejected        DelegationStatus = "rejected"
	DelegationCancelled       DelegationStatus = "cancelled"
)

type DelegationRequest struct {
	ID            DelegationRequestID
	SessionID     SessionID
	SourceAgentID AgentID
	Profile       AgentProfile
	ManifestID    ContextManifestID
	Grant         CapabilityGrant
	Status        DelegationStatus
	Approval      *ApprovalRecord
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewDelegationRequest(
	id DelegationRequestID,
	sessionID SessionID,
	sourceAgentID AgentID,
	profile AgentProfile,
	manifestID ContextManifestID,
	grant CapabilityGrant,
	at time.Time,
) (DelegationRequest, error) {
	request := DelegationRequest{
		ID:            id,
		SessionID:     sessionID,
		SourceAgentID: sourceAgentID,
		Profile:       profile,
		ManifestID:    manifestID,
		Grant:         grant.Snapshot(),
		Status:        DelegationDraft,
		CreatedAt:     at.UTC(),
		UpdatedAt:     at.UTC(),
	}
	if err := request.Validate(); err != nil {
		return DelegationRequest{}, err
	}
	return request, nil
}

func (r DelegationRequest) Validate() error {
	if idIsEmpty(string(r.ID)) || idIsEmpty(string(r.SessionID)) ||
		idIsEmpty(string(r.ManifestID)) {
		return invalidValue("delegationRequest", "required reference is missing")
	}
	if !r.Profile.Valid() {
		return invalidValue("delegationRequest.profile", "unknown agent profile")
	}
	if err := r.Grant.Validate(); err != nil {
		return fmtField("delegationRequest.grant", err)
	}
	if r.Grant.ContextManifestRef != r.ManifestID {
		return invalidValue("delegationRequest.manifestID", "manifest must match the grant reference")
	}
	if !validDelegationStatus(r.Status) {
		return invalidValue("delegationRequest.status", "unknown delegation status")
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return invalidValue("delegationRequest.timestamps", "timestamps are required")
	}
	return nil
}

func (r *DelegationRequest) Submit(at time.Time) (DomainEvent, error) {
	if r.Status != DelegationDraft {
		return DomainEvent{}, invalidTransition("delegation", string(r.Status), string(DelegationPendingApproval))
	}
	r.Status = DelegationPendingApproval
	r.UpdatedAt = at.UTC()
	event := newDomainEvent(EventDelegationPending, at)
	event.SessionID = r.SessionID
	event.AgentID = r.SourceAgentID
	event.DelegationID = r.ID
	return event, nil
}

func (r *DelegationRequest) Approve(approval ApprovalRecord, at time.Time) (DomainEvent, error) {
	if r.Status != DelegationPendingApproval {
		return DomainEvent{}, invalidTransition("delegation", string(r.Status), string(DelegationApproved))
	}
	if err := approval.Validate(); err != nil {
		return DomainEvent{}, err
	}
	r.Status = DelegationApproved
	copy := approval
	r.Approval = &copy
	r.UpdatedAt = at.UTC()
	event := newDomainEvent(EventDelegationApproved, at)
	event.SessionID = r.SessionID
	event.AgentID = r.SourceAgentID
	event.DelegationID = r.ID
	event.ApprovalSource = string(approval.Source)
	return event, nil
}

func (r *DelegationRequest) Reject(at time.Time) (DomainEvent, error) {
	if r.Status != DelegationPendingApproval {
		return DomainEvent{}, invalidTransition("delegation", string(r.Status), string(DelegationRejected))
	}
	r.Status = DelegationRejected
	r.UpdatedAt = at.UTC()
	event := newDomainEvent(EventDelegationRejected, at)
	event.SessionID, event.AgentID, event.DelegationID = r.SessionID, r.SourceAgentID, r.ID
	return event, nil
}

func (r *DelegationRequest) Cancel(at time.Time) (DomainEvent, error) {
	if r.Status != DelegationDraft && r.Status != DelegationPendingApproval {
		return DomainEvent{}, invalidTransition("delegation", string(r.Status), string(DelegationCancelled))
	}
	r.Status = DelegationCancelled
	r.UpdatedAt = at.UTC()
	event := newDomainEvent(EventDelegationCancelled, at)
	event.SessionID, event.AgentID, event.DelegationID = r.SessionID, r.SourceAgentID, r.ID
	return event, nil
}

func (r DelegationRequest) Snapshot() DelegationRequest {
	copy := r
	copy.Grant = r.Grant.Snapshot()
	if r.Approval != nil {
		approval := *r.Approval
		copy.Approval = &approval
	}
	return copy
}

func validDelegationStatus(status DelegationStatus) bool {
	switch status {
	case DelegationDraft, DelegationPendingApproval, DelegationApproved, DelegationRejected, DelegationCancelled:
		return true
	default:
		return false
	}
}
