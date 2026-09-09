package start

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	commandprotocol "praxis/internal/command"
	domainagent "praxis/internal/domain/agent"
	domaincontext "praxis/internal/domain/context"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
)

// MaterializeExecutionInput freezes the current context, policy and model
// selection into a durable input snapshot for queued and delivery work.
func (s *Service) MaterializeExecutionInput(ctx context.Context, agent domainagent.Agent, providerID, modelID, reasoningLevel string) (domainexecution.ExecutionInputSnapshot, error) {
	session, err := s.sessions.Get(ctx, agent.SessionID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	policy, err := s.policies.GetCurrent(ctx, agent.ID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	if policy.Revision != agent.SecurityPolicyRevision {
		return domainexecution.ExecutionInputSnapshot{}, domainfoundation.ErrRevisionConflict
	}
	model, err := s.models.ResolveModelSelection(providerID, modelID, reasoningLevel)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	contextRevision, err := s.contexts.CurrentRevision(ctx, agent.SessionID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	if contextRevision == 0 {
		return domainexecution.ExecutionInputSnapshot{}, errors.New("session context has no initial revision")
	}
	entries := make([]domaincontext.SessionContextEntry, 0, contextRevision)
	for after := uint64(0); after < contextRevision; {
		page, err := s.contexts.List(ctx, agent.SessionID, after, 512)
		if err != nil {
			return domainexecution.ExecutionInputSnapshot{}, err
		}
		if len(page) == 0 {
			return domainexecution.ExecutionInputSnapshot{}, errors.New("session context revision sequence is incomplete")
		}
		for _, entry := range page {
			if entry.Revision != uint64(len(entries)+1) {
				return domainexecution.ExecutionInputSnapshot{}, errors.New("session context revision sequence is incomplete")
			}
			entries = append(entries, entry)
		}
		after = page[len(page)-1].Revision
	}
	entryRevisions := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		entryRevisions = append(entryRevisions, entry.Revision)
	}
	contextSummary := boundedContextSummary(entries)
	manifest, err := domaincontext.NewContextManifest(domainfoundation.ContextManifestID(s.ids.New("manifest")), contextSummary, nil, s.clock.Now().UTC())
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	security, err := s.security.Resolve(policy, domainsecurity.ExecutionRestrictions{}, workspace, model, manifest)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	runtimeSnapshot, err := domainexecution.NewRuntimeExecutionSnapshot(
		security.Sandbox.Mode,
		security.ApprovalRules[0].Mode,
		security.Fingerprint,
	)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	selection := domainexecution.ContextSelection{Revision: contextRevision, EntryRevisions: entryRevisions, Summary: contextSummary}
	return domainexecution.ExecutionInputSnapshot{ContextManifest: manifest, ContextSelection: selection, Security: security, Runtime: runtimeSnapshot}, nil
}

func boundedContextSummary(entries []domaincontext.SessionContextEntry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if content := strings.TrimSpace(entry.Content); content != "" {
			parts = append(parts, content)
		}
	}
	joined := strings.Join(parts, "\n\n")
	if len([]byte(joined)) <= domaincontext.MaxManifestSummaryBytes {
		return joined
	}
	var bounded strings.Builder
	for _, value := range joined {
		size := utf8.RuneLen(value)
		if size < 0 || bounded.Len()+size > domaincontext.MaxManifestSummaryBytes {
			break
		}
		bounded.WriteRune(value)
	}
	return strings.TrimSpace(bounded.String())
}

func containsTool(tools []domainsecurity.ToolName, target domainsecurity.ToolName) bool {
	for _, tool := range tools {
		if tool == target {
			return true
		}
	}
	return false
}
