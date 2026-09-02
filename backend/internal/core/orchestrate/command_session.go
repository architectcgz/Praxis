package orchestrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	domaincontext "praxis/internal/core/domain/context"
	"strings"

	domainagent "praxis/internal/core/domain/agent"
	domaincommand "praxis/internal/core/domain/command"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainproject "praxis/internal/core/domain/project"
	domainsecurity "praxis/internal/core/domain/security"
	domainsession "praxis/internal/core/domain/session"
	domainworkspace "praxis/internal/core/domain/workspace"
)

type CreateSessionRequest struct {
	RequestID   domainfoundation.RequestID
	SessionID   domainfoundation.SessionID
	AgentID     domainfoundation.AgentID
	ProjectID   domainfoundation.ProjectID
	WorkspaceID domainfoundation.WorkspaceID
	Goal        string
	Profile     domainsecurity.AgentProfile
	Policy      domainsecurity.AgentSecurityPolicy
}

type CreateSessionResult struct {
	Session domainsession.Session
	Agent   domainagent.Agent
}

type CreateProjectRequest struct {
	RequestID   domainfoundation.RequestID
	ProjectID   domainfoundation.ProjectID
	WorkspaceID domainfoundation.WorkspaceID
	Name        string
	Path        string
}

type CreateProjectResult struct {
	Project   domainproject.Project
	Workspace domainworkspace.Workspace
}

type SessionSnapshot struct {
	ID          domainfoundation.SessionID
	ProjectID   domainfoundation.ProjectID
	WorkspaceID domainfoundation.WorkspaceID
	Goal        string
}

func (o *AgentOrchestrator) CreateProject(ctx context.Context, request CreateProjectRequest) (CreateProjectResult, error) {
	if ctx == nil {
		return CreateProjectResult{}, errors.New("create project context is required")
	}
	if !o.Ready() {
		return CreateProjectResult{}, commandError(CommandErrorNotReady)
	}
	if request.RequestID == "" {
		return CreateProjectResult{}, commandError(CommandErrorInvalidRequest)
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Path = filepath.Clean(strings.TrimSpace(request.Path))
	if request.Name == "" || strings.ContainsAny(request.Name, "\x00\r\n") || !absolutePath(request.Path) {
		return CreateProjectResult{}, commandError(CommandErrorProjectWorkspaceInvalid)
	}
	digest := commandArgumentsDigest(struct {
		ProjectID   domainfoundation.ProjectID
		WorkspaceID domainfoundation.WorkspaceID
		Name        string
		Path        string
	}{request.ProjectID, request.WorkspaceID, request.Name, request.Path})
	var result CreateProjectResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		if receipt, found, err := commandReceipt(txCtx, o.commandReceipts, request.RequestID, "create_project", digest); err != nil {
			return err
		} else if found {
			var ids struct {
				ProjectID   domainfoundation.ProjectID   `json:"projectId"`
				WorkspaceID domainfoundation.WorkspaceID `json:"workspaceId"`
			}
			if err := json.Unmarshal(receipt.ResultPayload, &ids); err != nil {
				return fmt.Errorf("decode create project receipt: %w", err)
			}
			project, err := o.projects.Get(txCtx, domainfoundation.ProjectID(ids.ProjectID))
			if err != nil {
				return err
			}
			workspace, err := o.workspaces.Get(txCtx, domainfoundation.WorkspaceID(ids.WorkspaceID))
			if err != nil {
				return err
			}
			result = CreateProjectResult{Project: project, Workspace: workspace}
			return nil
		}
		if request.ProjectID == "" {
			request.ProjectID = domainfoundation.ProjectID(o.newID("project"))
		}
		if request.WorkspaceID == "" {
			request.WorkspaceID = domainfoundation.WorkspaceID(o.newID("workspace"))
		}
		at := o.clock.Now().UTC()
		workspace, err := domainworkspace.NewWorkspace(request.WorkspaceID, request.ProjectID, domainworkspace.WorkspaceProjectRoot, request.Path, at)
		if err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		project, err := domainproject.NewProject(request.ProjectID, request.Name, request.Path, workspace.ID, at)
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := o.projects.Save(txCtx, project); err != nil {
			return err
		}
		if err := o.workspaces.Save(txCtx, workspace); err != nil {
			return err
		}
		event := o.newEvent(domainfoundation.EventProjectCreated, at)
		event.SessionID = ""
		event.Payload = map[string]string{"projectId": project.ID.String(), "workspaceId": workspace.ID.String()}
		if err := o.appendEvent(txCtx, event); err != nil {
			return err
		}
		result = CreateProjectResult{Project: project, Workspace: workspace}
		payload, _ := json.Marshal(struct {
			ProjectID   domainfoundation.ProjectID   `json:"projectId"`
			WorkspaceID domainfoundation.WorkspaceID `json:"workspaceId"`
		}{project.ID, workspace.ID})
		if err := o.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{
			RequestID: request.RequestID, Command: "create_project", ArgumentsDigest: digest,
			ResultPayload: payload, CreatedAt: at,
		}); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func (o *AgentOrchestrator) CreateSession(ctx context.Context, request CreateSessionRequest) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("create session context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	if request.RequestID == "" || request.ProjectID == "" || request.WorkspaceID == "" {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	if request.Profile == "" {
		request.Profile = domainsecurity.ProfilePrimary
	}
	if !request.Profile.Valid() {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	digest := commandArgumentsDigest(struct {
		SessionID   domainfoundation.SessionID
		AgentID     domainfoundation.AgentID
		ProjectID   domainfoundation.ProjectID
		WorkspaceID domainfoundation.WorkspaceID
		Goal        string
		Profile     domainsecurity.AgentProfile
	}{request.SessionID, request.AgentID, request.ProjectID, request.WorkspaceID, strings.TrimSpace(request.Goal), request.Profile})
	var result CreateSessionResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		if receipt, found, err := commandReceipt(txCtx, o.commandReceipts, request.RequestID, "create_session", digest); err != nil {
			return err
		} else if found {
			var ids struct{ SessionID, AgentID string }
			if err := json.Unmarshal(receipt.ResultPayload, &ids); err != nil {
				return fmt.Errorf("decode create session receipt: %w", err)
			}
			session, err := o.sessions.Get(txCtx, domainfoundation.SessionID(ids.SessionID))
			if err != nil {
				return err
			}
			agent, err := o.agents.Get(txCtx, domainfoundation.AgentID(ids.AgentID))
			if err != nil {
				return err
			}
			result = CreateSessionResult{Session: session, Agent: agent}
			return nil
		}
		project, err := o.projects.Get(txCtx, request.ProjectID)
		if err != nil || project.State != domainproject.ProjectActive {
			if err != nil {
				return err
			}
			return commandError(CommandErrorProjectWorkspaceInvalid)
		}
		workspace, err := o.workspaces.Get(txCtx, request.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.ProjectID != project.ID || workspace.State != domainworkspace.WorkspaceReady {
			return commandError(CommandErrorProjectWorkspaceInvalid)
		}
		if request.SessionID == "" {
			request.SessionID = domainfoundation.SessionID(o.newID("session"))
		}
		if request.AgentID == "" {
			request.AgentID = domainfoundation.AgentID(o.newID("agent"))
		}
		at := o.clock.Now().UTC()
		session, err := domainsession.NewSession(request.SessionID, project.ID, workspace.ID, request.Goal, at)
		if err != nil {
			return err
		}
		policy := request.Policy
		if policy.Revision == 0 {
			var policyErr error
			policy, policyErr = o.defaultAgentSecurityPolicy(workspace, request.Profile)
			if policyErr != nil {
				return policyErr
			}
		}
		agent, err := domainagent.NewAgent(request.AgentID, session.ID, request.Profile, policy.Revision, at)
		if err != nil {
			return err
		}
		if err := o.sessions.Save(txCtx, session); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		if err := o.policies.Save(txCtx, agent.ID, policy); err != nil {
			return err
		}
		entry, err := domaincontext.NewSessionContextEntry(session.ID, 1, domaincontext.SessionContextUserMessage, "", session.Goal, at)
		if err != nil {
			return err
		}
		if err := o.contexts.Append(txCtx, entry, 0); err != nil {
			return err
		}
		sessionEvent := o.newEvent(domainfoundation.EventSessionCreated, at)
		sessionEvent.SessionID, sessionEvent.AgentID = session.ID, agent.ID
		if err := o.appendEvent(txCtx, sessionEvent); err != nil {
			return err
		}
		agentEvent := o.newEvent(domainfoundation.EventAgentCreated, at)
		agentEvent.SessionID, agentEvent.AgentID = session.ID, agent.ID
		if err := o.appendEvent(txCtx, agentEvent); err != nil {
			return err
		}
		result = CreateSessionResult{Session: session, Agent: agent}
		payload, _ := json.Marshal(struct{ SessionID, AgentID string }{session.ID.String(), agent.ID.String()})
		if err := o.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{
			RequestID: request.RequestID, Command: "create_session", ArgumentsDigest: digest,
			ResultPayload: payload, CreatedAt: at,
		}); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func (o *AgentOrchestrator) CreateSessionForProject(ctx context.Context, requestID domainfoundation.RequestID, projectID domainfoundation.ProjectID, workspaceID domainfoundation.WorkspaceID, goal string) (CreateSessionResult, error) {
	return o.CreateSession(ctx, CreateSessionRequest{RequestID: requestID, ProjectID: projectID, WorkspaceID: workspaceID, Goal: goal, Profile: domainsecurity.ProfilePrimary})
}

func (o *AgentOrchestrator) EnsurePrimaryAgent(ctx context.Context, sessionID domainfoundation.SessionID, requestID domainfoundation.RequestID) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("ensure primary agent context is required")
	}
	if !o.Ready() || requestID == "" {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	session, err := o.sessions.Get(ctx, sessionID)
	if err != nil {
		return CreateSessionResult{}, err
	}
	agents, err := o.agents.ListBySession(ctx, sessionID, 1)
	if err != nil {
		return CreateSessionResult{}, err
	}
	if len(agents) > 0 {
		return CreateSessionResult{Session: session, Agent: agents[0]}, nil
	}
	workspace, err := o.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return CreateSessionResult{}, err
	}
	policy, err := o.defaultAgentSecurityPolicy(workspace, domainsecurity.ProfilePrimary)
	if err != nil {
		return CreateSessionResult{}, err
	}
	return o.CreateSession(ctx, CreateSessionRequest{RequestID: requestID, SessionID: session.ID, ProjectID: session.ProjectID, WorkspaceID: session.WorkspaceID, Goal: session.Goal, Profile: domainsecurity.ProfilePrimary, Policy: policy})
}

func (o *AgentOrchestrator) defaultAgentSecurityPolicy(workspace domainworkspace.Workspace, profile domainsecurity.AgentProfile) (domainsecurity.AgentSecurityPolicy, error) {
	if o.policyFactory != nil {
		return o.policyFactory(workspace, profile)
	}
	return domainsecurity.NewAgentSecurityPolicy(1, domainsecurity.CapabilityPolicy{
		AllowedTools: primaryTools(), ReadScopes: []string{workspace.Path},
	}, domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxReadOnly}, domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalAlwaysAsk})
}

func primaryTools() []domainsecurity.ToolName {
	return []domainsecurity.ToolName{domainsecurity.ToolReadFile, domainsecurity.ToolListDir, domainsecurity.ToolSearchText, domainsecurity.ToolProposeDelegate, domainsecurity.ToolSubmitResult, domainsecurity.ToolSubmitBriefing}
}

func absolutePath(path string) bool {
	return path != "" && path != "." && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}
