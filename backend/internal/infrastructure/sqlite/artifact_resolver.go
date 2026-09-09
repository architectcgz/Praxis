package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domaincontext "praxis/internal/domain/context"
	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"

	sessionport "praxis/internal/session"
)

// ResolveContextArtifact projects an approved result or briefing into the
// narrow structured payload accepted by DeliveryCoordinator. It never reads a
// transcript and does not expose provider payloads or secret-bearing fields.
func (s *Store) ResolveContextArtifact(
	ctx context.Context,
	delivery domainworkflow.ContextDelivery,
) (sessionport.ContextArtifact, error) {
	if ctx == nil {
		return sessionport.ContextArtifact{}, errors.New("context artifact resolver context is required")
	}
	if err := delivery.Validate(); err != nil {
		return sessionport.ContextArtifact{}, err
	}
	kind, id, explicit := parseArtifactReference(delivery.SourceArtifactID)
	if explicit {
		return s.resolveExplicitArtifact(ctx, delivery, kind, id)
	}
	result, resultErr := s.GetAgentResult(ctx, domainfoundation.AgentResultID(id))
	if resultErr == nil {
		return encodeAgentResultArtifact(delivery, result)
	}
	if !errors.Is(resultErr, domainfoundation.ErrNotFound) {
		return sessionport.ContextArtifact{}, resultErr
	}
	briefing, briefingErr := s.GetBriefing(ctx, domainfoundation.BriefingID(id))
	if briefingErr == nil {
		return encodeBriefingArtifact(delivery, briefing)
	}
	if errors.Is(briefingErr, domainfoundation.ErrNotFound) {
		return sessionport.ContextArtifact{}, domainfoundation.ErrNotFound
	}
	return sessionport.ContextArtifact{}, briefingErr
}

func (s *Store) resolveExplicitArtifact(
	ctx context.Context,
	delivery domainworkflow.ContextDelivery,
	kind, id string,
) (sessionport.ContextArtifact, error) {
	switch kind {
	case "agent_result":
		result, err := s.GetAgentResult(ctx, domainfoundation.AgentResultID(id))
		if err != nil {
			return sessionport.ContextArtifact{}, err
		}
		return encodeAgentResultArtifact(delivery, result)
	case "briefing":
		briefing, err := s.GetBriefing(ctx, domainfoundation.BriefingID(id))
		if err != nil {
			return sessionport.ContextArtifact{}, err
		}
		return encodeBriefingArtifact(delivery, briefing)
	default:
		return sessionport.ContextArtifact{}, fmt.Errorf("unsupported source artifact kind %q", kind)
	}
}

func encodeAgentResultArtifact(
	delivery domainworkflow.ContextDelivery,
	result domainworkflow.AgentResult,
) (sessionport.ContextArtifact, error) {
	if result.Status != domainworkflow.ArtifactApproved {
		return sessionport.ContextArtifact{}, errors.New("agent result is not approved")
	}
	if delivery.SessionID != result.SessionID {
		return sessionport.ContextArtifact{}, errors.New("agent result session does not match delivery")
	}
	body, err := json.Marshal(struct {
		Summary      string                     `json:"summary"`
		ChangedPaths []string                   `json:"changedPaths,omitempty"`
		EvidenceRefs []domaincontext.ContentRef `json:"evidenceRefs,omitempty"`
	}{
		Summary:      result.Summary,
		ChangedPaths: append([]string(nil), result.ChangedPaths...),
		EvidenceRefs: result.EvidenceRefs,
	})
	if err != nil {
		return sessionport.ContextArtifact{}, fmt.Errorf("encode agent result artifact: %w", err)
	}
	return sessionport.ContextArtifact{
		DeliveryID: delivery.ID,
		Kind:       "agent_result",
		ArtifactID: result.ID.String(),
		Body:       body,
	}, nil
}

func encodeBriefingArtifact(
	delivery domainworkflow.ContextDelivery,
	briefing domainworkflow.Briefing,
) (sessionport.ContextArtifact, error) {
	if briefing.Status != domainworkflow.ArtifactApproved {
		return sessionport.ContextArtifact{}, errors.New("briefing is not approved")
	}
	if delivery.SessionID != briefing.SessionID {
		return sessionport.ContextArtifact{}, errors.New("briefing session does not match delivery")
	}
	body, err := json.Marshal(struct {
		Body string `json:"body"`
	}{Body: briefing.Body})
	if err != nil {
		return sessionport.ContextArtifact{}, fmt.Errorf("encode briefing artifact: %w", err)
	}
	return sessionport.ContextArtifact{
		DeliveryID: delivery.ID,
		Kind:       "briefing",
		ArtifactID: briefing.ID.String(),
		Body:       body,
	}, nil
}

func parseArtifactReference(value string) (kind, id string, explicit bool) {
	value = strings.TrimSpace(value)
	for _, separator := range []string{":", "/"} {
		parts := strings.SplitN(value, separator, 2)
		if len(parts) != 2 {
			continue
		}
		kind = strings.TrimSpace(parts[0])
		id = strings.TrimSpace(parts[1])
		switch kind {
		case "agent_result", "agent-result", "result":
			return "agent_result", id, true
		case "briefing":
			return "briefing", id, true
		}
	}
	return "", value, false
}
