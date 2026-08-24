package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
)

// ResolveContextArtifact projects an approved result or briefing into the
// narrow structured payload accepted by DeliveryCoordinator. It never reads a
// transcript and does not expose provider payloads or secret-bearing fields.
func (s *Store) ResolveContextArtifact(
	ctx context.Context,
	delivery domain.ContextDelivery,
) (coresession.ContextArtifact, error) {
	if ctx == nil {
		return coresession.ContextArtifact{}, errors.New("context artifact resolver context is required")
	}
	if err := delivery.Validate(); err != nil {
		return coresession.ContextArtifact{}, err
	}
	kind, id, explicit := parseArtifactReference(delivery.SourceArtifactID)
	if explicit {
		return s.resolveExplicitArtifact(ctx, delivery, kind, id)
	}
	result, resultErr := s.GetAgentResult(ctx, domain.AgentResultID(id))
	if resultErr == nil {
		return encodeAgentResultArtifact(delivery, result)
	}
	if !errors.Is(resultErr, domain.ErrNotFound) {
		return coresession.ContextArtifact{}, resultErr
	}
	briefing, briefingErr := s.GetBriefing(ctx, domain.BriefingID(id))
	if briefingErr == nil {
		return encodeBriefingArtifact(delivery, briefing)
	}
	if errors.Is(briefingErr, domain.ErrNotFound) {
		return coresession.ContextArtifact{}, domain.ErrNotFound
	}
	return coresession.ContextArtifact{}, briefingErr
}

func (s *Store) resolveExplicitArtifact(
	ctx context.Context,
	delivery domain.ContextDelivery,
	kind, id string,
) (coresession.ContextArtifact, error) {
	switch kind {
	case "agent_result":
		result, err := s.GetAgentResult(ctx, domain.AgentResultID(id))
		if err != nil {
			return coresession.ContextArtifact{}, err
		}
		return encodeAgentResultArtifact(delivery, result)
	case "briefing":
		briefing, err := s.GetBriefing(ctx, domain.BriefingID(id))
		if err != nil {
			return coresession.ContextArtifact{}, err
		}
		return encodeBriefingArtifact(delivery, briefing)
	default:
		return coresession.ContextArtifact{}, fmt.Errorf("unsupported source artifact kind %q", kind)
	}
}

func encodeAgentResultArtifact(
	delivery domain.ContextDelivery,
	result domain.AgentResult,
) (coresession.ContextArtifact, error) {
	if result.Status != domain.ArtifactApproved {
		return coresession.ContextArtifact{}, errors.New("agent result is not approved")
	}
	if domain.TaskSessionID(delivery.SessionID) != result.TaskSessionID {
		return coresession.ContextArtifact{}, errors.New("agent result session does not match delivery")
	}
	body, err := json.Marshal(struct {
		Summary      string              `json:"summary"`
		ChangedPaths []string            `json:"changedPaths,omitempty"`
		EvidenceRefs []domain.ContentRef `json:"evidenceRefs,omitempty"`
	}{
		Summary:      result.Summary,
		ChangedPaths: append([]string(nil), result.ChangedPaths...),
		EvidenceRefs: result.EvidenceRefs,
	})
	if err != nil {
		return coresession.ContextArtifact{}, fmt.Errorf("encode agent result artifact: %w", err)
	}
	return coresession.ContextArtifact{
		DeliveryID: delivery.ID,
		Kind:       "agent_result",
		ArtifactID: result.ID.String(),
		Body:       body,
	}, nil
}

func encodeBriefingArtifact(
	delivery domain.ContextDelivery,
	briefing domain.Briefing,
) (coresession.ContextArtifact, error) {
	if briefing.Status != domain.ArtifactApproved {
		return coresession.ContextArtifact{}, errors.New("briefing is not approved")
	}
	if domain.TaskSessionID(delivery.SessionID) != briefing.TaskSessionID {
		return coresession.ContextArtifact{}, errors.New("briefing session does not match delivery")
	}
	body, err := json.Marshal(struct {
		Body string `json:"body"`
	}{Body: briefing.Body})
	if err != nil {
		return coresession.ContextArtifact{}, fmt.Errorf("encode briefing artifact: %w", err)
	}
	return coresession.ContextArtifact{
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
