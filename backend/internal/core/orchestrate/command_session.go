package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"praxis/internal/core/domain"
)

// CreateSessionRequest identifies the project and workspace explicitly. A
// caller may provide the immutable input aggregates; when omitted, the
// orchestrator creates the standard primary-agent inputs.
type CreateSessionRequest struct {
	SessionID       domain.SessionID
	GroupID         domain.AgentGroupID
	AgentID         domain.AgentID
	ProjectID       domain.ProjectID
	WorkspaceID     domain.WorkspaceID
	Goal            string
	MaxConcurrent   int
	Profile         domain.AgentProfile
	TaskPacket      domain.TaskPacket
	ContextManifest domain.ContextManifest
	Grant           domain.CapabilityGrant
}

type CreateSessionResult struct {
	Session SessionSnapshot
	Group   domain.AgentGroup
	Agent   domain.Agent
}

// CreateProjectRequest provisions one Project and its default Workspace.
type CreateProjectRequest struct {
	ProjectID   domain.ProjectID
	WorkspaceID domain.WorkspaceID
	Name        string
	Path        string
}

type CreateProjectResult struct {
	Project   domain.Project
	Workspace domain.Workspace
}

type SessionSnapshot struct {
	ID          domain.SessionID
	ProjectID   domain.ProjectID
	WorkspaceID domain.WorkspaceID
	Goal        string
}

func (o *AgentOrchestrator) CreateProject(
	ctx context.Context,
	request CreateProjectRequest,
) (CreateProjectResult, error) {
	if ctx == nil {
		return CreateProjectResult{}, errors.New("create project context is required")
	}
	if !o.Ready() {
		return CreateProjectResult{}, commandError(CommandErrorNotReady)
	}
	if request.ProjectID == "" {
		request.ProjectID = domain.NewProjectID()
	}
	if request.WorkspaceID == "" {
		request.WorkspaceID = domain.NewWorkspaceID()
	}
	name := strings.TrimSpace(request.Name)
	path := filepath.Clean(strings.TrimSpace(request.Path))
	if name == "" || strings.ContainsAny(name, "\x00\r\n") || !absolutePath(path) {
		return CreateProjectResult{}, commandError(CommandErrorProjectWorkspaceInvalid)
	}
	at := o.clock.Now().UTC()
	workspace, err := domain.NewWorkspace(
		request.WorkspaceID,
		request.ProjectID,
		domain.WorkspaceProjectRoot,
		path,
		at,
	)
	if err != nil {
		return CreateProjectResult{}, fmt.Errorf("create workspace: %w", err)
	}
	project, err := domain.NewProject(request.ProjectID, name, workspace.ID, at)
	if err != nil {
		return CreateProjectResult{}, fmt.Errorf("create project: %w", err)
	}
	if err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		if err := o.projects.Save(txCtx, project); err != nil {
			return err
		}
		return o.workspaces.Save(txCtx, workspace)
	}); err != nil {
		return CreateProjectResult{}, err
	}
	return CreateProjectResult{Project: project, Workspace: workspace}, nil
}

// CreateSession creates one independent Session and its initial AgentGroup and
// primary Agent. Project and workspace ownership is checked in the same
// transaction as the new records.
func (o *AgentOrchestrator) CreateSession(
	ctx context.Context,
	request CreateSessionRequest,
) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("create session context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	if request.SessionID == "" {
		request.SessionID = domain.NewSessionID()
	}
	if request.GroupID == "" {
		request.GroupID = domain.NewAgentGroupID()
	}
	if request.AgentID == "" {
		request.AgentID = domain.NewAgentID()
	}
	if request.MaxConcurrent < 1 {
		request.MaxConcurrent = 1
	}
	if request.Profile == "" {
		request.Profile = domain.ProfilePrimary
	}
	if request.ProjectID == "" || request.WorkspaceID == "" || !request.Profile.Valid() {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	result := CreateSessionResult{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		project, err := o.projects.Get(txCtx, request.ProjectID)
		if err != nil {
			return err
		}
		if project.State != domain.ProjectActive {
			return commandError(CommandErrorProjectWorkspaceInvalid)
		}
		workspace, err := o.workspaces.Get(txCtx, request.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.ProjectID != project.ID || workspace.State != domain.WorkspaceReady {
			return commandError(CommandErrorProjectWorkspaceInvalid)
		}
		packet, manifest, grant, err := o.sessionInputs(request, workspace)
		if err != nil {
			return err
		}
		at := o.clock.Now().UTC()
		session, err := domain.NewSession(request.SessionID, project.ID, workspace.ID, request.Goal, at)
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		group, err := domain.NewAgentGroup(request.GroupID, session.ID, request.MaxConcurrent, at)
		if err != nil {
			return err
		}
		agent, err := domain.NewAgent(
			request.AgentID,
			session.ID,
			group.ID,
			request.Profile,
			packet.ID,
			manifest.ID,
			grant.ID,
			at,
		)
		if err != nil {
			return err
		}
		if request.Profile == domain.ProfilePrimary {
			if err := group.SetPrimary(agent.ID, at); err != nil {
				return err
			}
		}
		for _, save := range []func(context.Context) error{
			func(c context.Context) error { return o.packets.Save(c, packet) },
			func(c context.Context) error { return o.manifests.Save(c, manifest) },
			func(c context.Context) error { return o.grants.Save(c, grant) },
			func(c context.Context) error { return o.sessions.Save(c, session) },
			func(c context.Context) error { return o.groups.Save(c, group) },
			func(c context.Context) error { return o.agents.Save(c, agent) },
		} {
			if err := save(txCtx); err != nil {
				return err
			}
		}
		result = CreateSessionResult{
			Session: sessionSnapshot(session),
			Group:   group,
			Agent:   agent,
		}
		return nil
	})
	if err != nil {
		return CreateSessionResult{}, err
	}
	return result, nil
}

// CreateSessionForProject is the compact command used by the desktop binding.
func (o *AgentOrchestrator) CreateSessionForProject(
	ctx context.Context,
	projectID domain.ProjectID,
	workspaceID domain.WorkspaceID,
	goal string,
) (CreateSessionResult, error) {
	return o.CreateSession(ctx, CreateSessionRequest{
		ProjectID: projectID, WorkspaceID: workspaceID, Goal: goal,
		Profile: domain.ProfilePrimary, MaxConcurrent: 1,
	})
}

// EnsurePrimaryAgent repairs a session created without an agent, while
// preserving the same project/workspace identity.
func (o *AgentOrchestrator) EnsurePrimaryAgent(
	ctx context.Context,
	sessionID domain.SessionID,
) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("ensure primary agent context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	session, err := o.sessions.Get(ctx, sessionID)
	if err != nil {
		return CreateSessionResult{}, err
	}
	groups, err := o.groups.ListBySession(ctx, sessionID, 100)
	if err != nil {
		return CreateSessionResult{}, err
	}
	for _, group := range groups {
		agents, listErr := o.agents.ListByGroup(ctx, group.ID, 100)
		if listErr != nil {
			return CreateSessionResult{}, listErr
		}
		if len(agents) > 0 {
			return CreateSessionResult{Session: sessionSnapshot(session), Group: group, Agent: agents[0]}, nil
		}
	}
	workspace, err := o.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return CreateSessionResult{}, err
	}
	packet, manifest, grant, err := o.sessionInputs(CreateSessionRequest{Goal: session.Goal, Profile: domain.ProfilePrimary}, workspace)
	if err != nil {
		return CreateSessionResult{}, err
	}
	at := o.clock.Now().UTC()
	group, err := domain.NewAgentGroup(domain.NewAgentGroupID(), session.ID, 1, at)
	if err != nil {
		return CreateSessionResult{}, err
	}
	agent, err := domain.NewAgent(domain.NewAgentID(), session.ID, group.ID, domain.ProfilePrimary, packet.ID, manifest.ID, grant.ID, at)
	if err != nil {
		return CreateSessionResult{}, err
	}
	if err := group.SetPrimary(agent.ID, at); err != nil {
		return CreateSessionResult{}, err
	}
	if err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		for _, save := range []func(context.Context) error{
			func(c context.Context) error { return o.packets.Save(c, packet) },
			func(c context.Context) error { return o.manifests.Save(c, manifest) },
			func(c context.Context) error { return o.grants.Save(c, grant) },
			func(c context.Context) error { return o.groups.Save(c, group) },
			func(c context.Context) error { return o.agents.Save(c, agent) },
		} {
			if err := save(txCtx); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return CreateSessionResult{}, err
	}
	return CreateSessionResult{Session: sessionSnapshot(session), Group: group, Agent: agent}, nil
}

func (o *AgentOrchestrator) sessionInputs(
	request CreateSessionRequest,
	workspace domain.Workspace,
) (domain.TaskPacket, domain.ContextManifest, domain.CapabilityGrant, error) {
	packet := request.TaskPacket
	manifest := request.ContextManifest
	grant := request.Grant
	at := o.clock.Now().UTC()
	if packet.ID == "" {
		var err error
		packet, err = domain.NewTaskPacket(domain.NewTaskPacketID(), strings.TrimSpace(request.Goal), nil, nil)
		if err != nil {
			return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, err
		}
	}
	if manifest.ID == "" {
		var err error
		manifest, err = domain.NewContextManifest(domain.NewContextManifestID(), packet.Goal, nil, at)
		if err != nil {
			return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, err
		}
	}
	if grant.ID == "" {
		if o.models == nil {
			return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, commandError(CommandErrorModelNotConfigured)
		}
		model, err := o.models.ResolveModel(domain.ProfilePrimary)
		if err != nil {
			return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, fmt.Errorf("%w: %v", commandError(CommandErrorModelNotConfigured), err)
		}
		grant, err = domain.NewCapabilityGrant(domain.CapabilityGrantSpec{
			ID:                    domain.NewCapabilityGrantID(),
			WorkspaceID:           workspace.ID,
			WorkspacePathSnapshot: workspace.Path,
			WorkspaceRevision:     workspace.Revision,
			AllowedTools:          primaryTools(),
			ReadScopes:            []string{workspace.Path},
			CanProposeDelegation:  true,
			ResultPermissions:     []domain.ResultPermission{domain.ResultPermissionAgentResult, domain.ResultPermissionBriefing},
			Model:                 model,
			ContextManifestRef:    manifest.ID,
			ApprovalSource:        domain.ApprovalSourceUser,
		})
		if err != nil {
			return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, err
		}
	}
	if err := packet.Validate(); err != nil {
		return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, err
	}
	if err := manifest.Validate(); err != nil {
		return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, err
	}
	if err := grant.Validate(); err != nil {
		return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, err
	}
	if grant.ContextManifestRef != manifest.ID || grant.WorkspaceID != workspace.ID ||
		grant.WorkspacePathSnapshot != workspace.Path || grant.WorkspaceRevision != workspace.Revision {
		return domain.TaskPacket{}, domain.ContextManifest{}, domain.CapabilityGrant{}, commandError(CommandErrorInvalidRequest)
	}
	return packet, manifest, grant.Snapshot(), nil
}

func primaryTools() []domain.ToolName {
	return []domain.ToolName{
		domain.ToolReadFile,
		domain.ToolListDir,
		domain.ToolSearchText,
		domain.ToolProposeDelegate,
		domain.ToolSubmitResult,
		domain.ToolSubmitBriefing,
	}
}

func sessionSnapshot(session domain.Session) SessionSnapshot {
	return SessionSnapshot{
		ID: session.ID, ProjectID: session.ProjectID, WorkspaceID: session.WorkspaceID, Goal: session.Goal,
	}
}

func absolutePath(path string) bool {
	return path != "" && path != "." && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsAny(path, "\x00\r\n")
}
