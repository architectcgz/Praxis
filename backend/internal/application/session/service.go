// Package session owns Session initialization and primary Agent admission.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	corecommand "praxis/internal/core/command"
	domainagent "praxis/internal/core/domain/agent"
	domaincommand "praxis/internal/core/domain/command"
	domaincontext "praxis/internal/core/domain/context"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainproject "praxis/internal/core/domain/project"
	domainsecurity "praxis/internal/core/domain/security"
	domainsession "praxis/internal/core/domain/session"
	domainworkspace "praxis/internal/core/domain/workspace"
	"praxis/internal/core/persistence"
	"praxis/internal/core/system"
)

// Readiness controls command admission while recovery or shutdown converges.
type Readiness interface {
	Ready() bool
}

// AgentSecurityPolicyFactory resolves the initial immutable policy revision.
type AgentSecurityPolicyFactory func(domainworkspace.Workspace, domainsecurity.AgentProfile) (domainsecurity.AgentSecurityPolicy, error)

// Config contains the ports required to initialize a Session and primary Agent.
type Config struct {
	Transactions    persistence.TxRunner
	Projects        persistence.ProjectRepository
	Workspaces      persistence.WorkspaceRepository
	Sessions        persistence.SessionRepository
	Contexts        persistence.SessionContextRepository
	Policies        persistence.AgentSecurityPolicyRepository
	Agents          persistence.AgentRepository
	CommandReceipts persistence.CommandReceiptRepository
	Events          persistence.EventRepository
	Readiness       Readiness
	PolicyFactory   AgentSecurityPolicyFactory
	Clock           system.Clock
	IDs             system.IDGenerator
}

// Service owns Session initialization and primary Agent lookup.
type Service struct {
	tx              persistence.TxRunner
	projects        persistence.ProjectRepository
	workspaces      persistence.WorkspaceRepository
	sessions        persistence.SessionRepository
	contexts        persistence.SessionContextRepository
	policies        persistence.AgentSecurityPolicyRepository
	agents          persistence.AgentRepository
	commandReceipts persistence.CommandReceiptRepository
	events          persistence.EventRepository
	readiness       Readiness
	policyFactory   AgentSecurityPolicyFactory
	clock           system.Clock
	ids             system.IDGenerator
}

// CreateParams contains the durable identity and initial state for a Session.
type CreateParams struct {
	RequestID   domainfoundation.RequestID
	SessionID   domainfoundation.SessionID
	AgentID     domainfoundation.AgentID
	ProjectID   domainfoundation.ProjectID
	WorkspaceID domainfoundation.WorkspaceID
	Goal        string
	Profile     domainsecurity.AgentProfile
	Policy      domainsecurity.AgentSecurityPolicy
}

// CreateResult returns the initialized Session and primary Agent.
type CreateResult struct {
	Session domainsession.Session
	Agent   domainagent.Agent
}

// NewService creates the Session application service.
func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "transactions", value: config.Transactions},
		{name: "projects", value: config.Projects},
		{name: "workspaces", value: config.Workspaces},
		{name: "sessions", value: config.Sessions},
		{name: "session contexts", value: config.Contexts},
		{name: "security policies", value: config.Policies},
		{name: "agents", value: config.Agents},
		{name: "command receipts", value: config.CommandReceipts},
		{name: "events", value: config.Events},
		{name: "readiness", value: config.Readiness},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("session service %s is required", required.name)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	ids := config.IDs
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	return &Service{
		tx:              config.Transactions,
		projects:        config.Projects,
		workspaces:      config.Workspaces,
		sessions:        config.Sessions,
		contexts:        config.Contexts,
		policies:        config.Policies,
		agents:          config.Agents,
		commandReceipts: config.CommandReceipts,
		events:          config.Events,
		readiness:       config.Readiness,
		policyFactory:   config.PolicyFactory,
		clock:           clock,
		ids:             ids,
	}, nil
}

// CreateSession creates a Session, its primary Agent, initial policy and
// revision-one context in one transaction.
func (s *Service) CreateSession(ctx context.Context, params CreateParams) (CreateResult, error) {
	if ctx == nil {
		return CreateResult{}, errors.New("create session context is required")
	}
	if !s.readiness.Ready() {
		return CreateResult{}, corecommand.NewError(corecommand.ErrorNotReady)
	}
	if params.RequestID == "" || params.ProjectID == "" || params.WorkspaceID == "" {
		return CreateResult{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
	}
	if params.Profile == "" {
		params.Profile = domainsecurity.ProfilePrimary
	}
	if !params.Profile.Valid() {
		return CreateResult{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
	}
	digest := corecommand.ArgumentsDigest(struct {
		SessionID   domainfoundation.SessionID
		AgentID     domainfoundation.AgentID
		ProjectID   domainfoundation.ProjectID
		WorkspaceID domainfoundation.WorkspaceID
		Goal        string
		Profile     domainsecurity.AgentProfile
	}{params.SessionID, params.AgentID, params.ProjectID, params.WorkspaceID, strings.TrimSpace(params.Goal), params.Profile})
	var result CreateResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		if receipt, found, err := corecommand.FindReceipt(txCtx, s.commandReceipts, params.RequestID, "create_session", digest); err != nil {
			return err
		} else if found {
			var ids struct{ SessionID, AgentID string }
			if err := json.Unmarshal(receipt.ResultPayload, &ids); err != nil {
				return fmt.Errorf("decode create session receipt: %w", err)
			}
			session, err := s.sessions.Get(txCtx, domainfoundation.SessionID(ids.SessionID))
			if err != nil {
				return err
			}
			agent, err := s.agents.Get(txCtx, domainfoundation.AgentID(ids.AgentID))
			if err != nil {
				return err
			}
			result = CreateResult{Session: session, Agent: agent}
			return nil
		}
		project, err := s.projects.Get(txCtx, params.ProjectID)
		if err != nil || project.State != domainproject.ProjectActive {
			if err != nil {
				return err
			}
			return corecommand.NewError(corecommand.ErrorProjectWorkspaceInvalid)
		}
		workspace, err := s.workspaces.Get(txCtx, params.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.ProjectID != project.ID || workspace.State != domainworkspace.WorkspaceReady {
			return corecommand.NewError(corecommand.ErrorProjectWorkspaceInvalid)
		}
		if params.SessionID == "" {
			params.SessionID = domainfoundation.SessionID(s.ids.New("session"))
		}
		if params.AgentID == "" {
			params.AgentID = domainfoundation.AgentID(s.ids.New("agent"))
		}
		at := s.clock.Now().UTC()
		session, err := domainsession.NewSession(params.SessionID, project.ID, workspace.ID, params.Goal, at)
		if err != nil {
			return err
		}
		policy := params.Policy
		if policy.Revision == 0 {
			policy, err = s.defaultAgentSecurityPolicy(workspace, params.Profile)
			if err != nil {
				return err
			}
		}
		agent, err := domainagent.NewAgent(params.AgentID, session.ID, params.Profile, policy.Revision, at)
		if err != nil {
			return err
		}
		if err := s.sessions.Save(txCtx, session); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		if err := s.policies.Save(txCtx, agent.ID, policy); err != nil {
			return err
		}
		entry, err := domaincontext.NewSessionContextEntry(session.ID, 1, domaincontext.SessionContextUserMessage, "", session.Goal, at)
		if err != nil {
			return err
		}
		if err := s.contexts.Append(txCtx, entry, 0); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, domainfoundation.EventSessionCreated, at, session.ID, agent.ID); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, domainfoundation.EventAgentCreated, at, session.ID, agent.ID); err != nil {
			return err
		}
		payload, _ := json.Marshal(struct{ SessionID, AgentID string }{session.ID.String(), agent.ID.String()})
		if err := s.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{
			RequestID: params.RequestID, Command: "create_session", ArgumentsDigest: digest,
			ResultPayload: payload, CreatedAt: at,
		}); err != nil {
			return err
		}
		result = CreateResult{Session: session, Agent: agent}
		return nil
	})
	return result, err
}

// CreateSessionForProject initializes a primary Session for one Project.
func (s *Service) CreateSessionForProject(ctx context.Context, requestID domainfoundation.RequestID, projectID domainfoundation.ProjectID, workspaceID domainfoundation.WorkspaceID, goal string) (CreateResult, error) {
	return s.CreateSession(ctx, CreateParams{
		RequestID: requestID, ProjectID: projectID, WorkspaceID: workspaceID,
		Goal: goal, Profile: domainsecurity.ProfilePrimary,
	})
}

// GetOrCreatePrimaryAgent returns an existing Session Agent or atomically
// creates the missing primary Agent through the Session initialization use case.
func (s *Service) GetOrCreatePrimaryAgent(ctx context.Context, sessionID domainfoundation.SessionID, requestID domainfoundation.RequestID) (domainagent.Agent, error) {
	if ctx == nil {
		return domainagent.Agent{}, errors.New("ensure primary agent context is required")
	}
	if !s.readiness.Ready() || requestID == "" {
		return domainagent.Agent{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return domainagent.Agent{}, err
	}
	agents, err := s.agents.ListBySession(ctx, sessionID, 1)
	if err != nil {
		return domainagent.Agent{}, err
	}
	if len(agents) > 0 {
		return agents[0], nil
	}
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return domainagent.Agent{}, err
	}
	policy, err := s.defaultAgentSecurityPolicy(workspace, domainsecurity.ProfilePrimary)
	if err != nil {
		return domainagent.Agent{}, err
	}
	result, err := s.CreateSession(ctx, CreateParams{
		RequestID: requestID, SessionID: session.ID, ProjectID: session.ProjectID,
		WorkspaceID: session.WorkspaceID, Goal: session.Goal,
		Profile: domainsecurity.ProfilePrimary, Policy: policy,
	})
	if err != nil {
		return domainagent.Agent{}, err
	}
	return result.Agent, nil
}

// AppendContextParams describes one compare-and-append SessionContext write.
type AppendContextParams struct {
	RequestID         domainfoundation.RequestID
	SessionID         domainfoundation.SessionID
	ExpectedRevision  uint64
	Kind              domaincontext.SessionContextKind
	SourceExecutionID domainfoundation.AgentExecutionID
	Content           string
}

// AppendContextResult returns the durable entry and retry state.
type AppendContextResult struct {
	Entry           domaincontext.SessionContextEntry
	ExistingRequest bool
}

// AppendSessionContext appends immutable shared context after checking the
// expected revision in the same transaction as its event and command receipt.
func (s *Service) AppendSessionContext(ctx context.Context, params AppendContextParams) (AppendContextResult, error) {
	if ctx == nil {
		return AppendContextResult{}, errors.New("append session context is required")
	}
	if !s.readiness.Ready() || params.RequestID == "" || params.SessionID == "" || strings.TrimSpace(params.Content) == "" {
		return AppendContextResult{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
	}
	digest := corecommand.ArgumentsDigest(struct {
		SessionID domainfoundation.SessionID
		Revision  uint64
		Kind      domaincontext.SessionContextKind
		Source    domainfoundation.AgentExecutionID
		Content   string
	}{params.SessionID, params.ExpectedRevision, params.Kind, params.SourceExecutionID, strings.TrimSpace(params.Content)})
	var result AppendContextResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		if receipt, found, err := corecommand.FindReceipt(txCtx, s.commandReceipts, params.RequestID, "append_session_context", digest); err != nil {
			return err
		} else if found {
			var value struct {
				SessionID string
				Revision  uint64
			}
			if err := json.Unmarshal(receipt.ResultPayload, &value); err != nil {
				return err
			}
			entries, err := s.contexts.List(txCtx, domainfoundation.SessionID(value.SessionID), value.Revision-1, 1)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return domainfoundation.ErrNotFound
			}
			result = AppendContextResult{Entry: entries[0], ExistingRequest: true}
			return nil
		}
		current, err := s.contexts.CurrentRevision(txCtx, params.SessionID)
		if err != nil {
			return err
		}
		if current != params.ExpectedRevision {
			return domainfoundation.ErrRevisionConflict
		}
		entry, err := domaincontext.NewSessionContextEntry(
			params.SessionID,
			current+1,
			params.Kind,
			params.SourceExecutionID,
			strings.TrimSpace(params.Content),
			s.clock.Now(),
		)
		if err != nil {
			return err
		}
		if err := s.contexts.Append(txCtx, entry, current); err != nil {
			return err
		}
		event := domainfoundation.DomainEvent{
			ID:         domainfoundation.EventID(s.ids.New("event")),
			Type:       domainfoundation.EventSessionContextAppended,
			SessionID:  entry.SessionID,
			OccurredAt: entry.CreatedAt.UTC(),
			Payload: map[string]string{
				"revision": strconv.FormatUint(entry.Revision, 10),
				"kind":     string(entry.Kind),
			},
		}
		if err := s.events.Append(txCtx, event); err != nil {
			return err
		}
		payload, _ := json.Marshal(struct {
			SessionID string
			Revision  uint64
		}{entry.SessionID.String(), entry.Revision})
		if err := s.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{
			RequestID: params.RequestID, Command: "append_session_context", ArgumentsDigest: digest,
			ResultPayload: payload, CreatedAt: entry.CreatedAt,
		}); err != nil {
			return err
		}
		result.Entry = entry
		return nil
	})
	return result, err
}

func (s *Service) defaultAgentSecurityPolicy(workspace domainworkspace.Workspace, profile domainsecurity.AgentProfile) (domainsecurity.AgentSecurityPolicy, error) {
	if s.policyFactory != nil {
		return s.policyFactory(workspace, profile)
	}
	return domainsecurity.NewAgentSecurityPolicy(1, domainsecurity.CapabilityPolicy{
		AllowedTools: primaryTools(), ReadScopes: []string{workspace.Path},
	}, domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxReadOnly}, domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalAlwaysAsk})
}

func (s *Service) appendEvent(ctx context.Context, eventType domainfoundation.DomainEventType, at time.Time, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID) error {
	return s.events.Append(ctx, domainfoundation.DomainEvent{
		ID:         domainfoundation.EventID(s.ids.New("event")),
		Type:       eventType,
		SessionID:  sessionID,
		AgentID:    agentID,
		OccurredAt: at.UTC(),
	})
}

func primaryTools() []domainsecurity.ToolName {
	return []domainsecurity.ToolName{
		domainsecurity.ToolReadFile,
		domainsecurity.ToolListDir,
		domainsecurity.ToolSearchText,
		domainsecurity.ToolProposeDelegate,
		domainsecurity.ToolSubmitResult,
		domainsecurity.ToolSubmitBriefing,
	}
}
