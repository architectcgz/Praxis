package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"praxis/internal/core/domain"
)

type CreateSessionRequest struct {
	SessionID       domain.SessionID
	GroupID         domain.AgentGroupID
	AgentID         domain.AgentID
	Goal            string
	WorkspaceKey    string
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

type CreateProjectRequest struct {
	WorkspaceKey string
	Goal         string
}

type SessionSnapshot struct {
	ID           domain.SessionID
	Goal         string
	WorkspaceKey string
}

// CreateSession creates the minimum explicit Session -> AgentGroup -> Agent
// hierarchy and saves every execution input reference in the same transaction.
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
	if request.MaxConcurrent < 1 || !request.Profile.Valid() {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.TaskPacket.Validate(); err != nil {
		return CreateSessionResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	if err := request.ContextManifest.Validate(); err != nil {
		return CreateSessionResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	if err := request.Grant.Validate(); err != nil {
		return CreateSessionResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	if request.Grant.ContextManifestRef != request.ContextManifest.ID {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	result := CreateSessionResult{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		at := o.clock.Now()
		session, err := domain.NewSession(request.SessionID, request.Goal, request.WorkspaceKey, at)
		if err != nil {
			return err
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
			request.TaskPacket.ID,
			request.ContextManifest.ID,
			request.Grant.ID,
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
		if err := o.packets.Save(txCtx, request.TaskPacket); err != nil {
			return err
		}
		if err := o.manifests.Save(txCtx, request.ContextManifest); err != nil {
			return err
		}
		if err := o.grants.Save(txCtx, request.Grant.Snapshot()); err != nil {
			return err
		}
		if err := o.sessions.Save(txCtx, session); err != nil {
			return err
		}
		if err := o.groups.Save(txCtx, group); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result = CreateSessionResult{
			Session: SessionSnapshot{ID: session.ID, Goal: session.Goal, WorkspaceKey: session.WorkspaceKey},
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

// CreateProject creates a workspace-backed Session with its first Primary Agent.
// The initial grant is intentionally read-only until the user approves broader access.
func (o *AgentOrchestrator) CreateProject(
	ctx context.Context,
	request CreateProjectRequest,
) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("create project context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	workspaceKey := filepath.Clean(strings.TrimSpace(request.WorkspaceKey))
	if workspaceKey == "." || !filepath.IsAbs(workspaceKey) || strings.ContainsAny(workspaceKey, "\x00\r\n") {
		return CreateSessionResult{}, commandError(CommandErrorProjectWorkspaceInvalid)
	}
	return o.createPrimarySession(ctx, workspaceKey, request.Goal)
}

// CreateSessionForWorkspace creates another Primary Agent session for an
// existing project workspace selected from the project catalog.
func (o *AgentOrchestrator) CreateSessionForWorkspace(
	ctx context.Context,
	workspaceKey string,
	goal string,
) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("create session context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	workspaceKey = filepath.Clean(strings.TrimSpace(workspaceKey))
	if workspaceKey == "." || !filepath.IsAbs(workspaceKey) || strings.ContainsAny(workspaceKey, "\x00\r\n") {
		return CreateSessionResult{}, commandError(CommandErrorProjectWorkspaceInvalid)
	}
	return o.createPrimarySession(ctx, workspaceKey, goal)
}

func (o *AgentOrchestrator) createPrimarySession(
	ctx context.Context,
	workspaceKey string,
	goal string,
) (CreateSessionResult, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = "Work on " + filepath.Base(workspaceKey)
	}
	at := o.clock.Now().UTC()
	packet, err := domain.NewTaskPacket(domain.NewTaskPacketID(), goal, nil, nil)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("create project task packet: %w", err)
	}
	manifest, err := domain.NewContextManifest(domain.NewContextManifestID(), goal, nil, at)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("create project context manifest: %w", err)
	}
	model, err := o.models.ResolveModel(domain.ProfilePrimary)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("resolve primary model: %w", err)
	}
	grant, err := domain.NewCapabilityGrant(domain.CapabilityGrantSpec{
		ID:           domain.NewCapabilityGrantID(),
		WorkspaceKey: workspaceKey,
		AllowedTools: []domain.ToolName{domain.ToolReadFile, domain.ToolListDir, domain.ToolSearchText,
			domain.ToolProposeDelegate, domain.ToolSubmitResult, domain.ToolSubmitBriefing},
		ReadScopes:           []string{workspaceKey},
		CanProposeDelegation: true,
		ResultPermissions: []domain.ResultPermission{
			domain.ResultPermissionAgentResult,
			domain.ResultPermissionBriefing,
		},
		Model:              model,
		ContextManifestRef: manifest.ID,
		ApprovalSource:     domain.ApprovalSourceUser,
	})
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("create project capability grant: %w", err)
	}
	return o.CreateSession(ctx, CreateSessionRequest{
		Goal:            goal,
		WorkspaceKey:    workspaceKey,
		MaxConcurrent:   1,
		Profile:         domain.ProfilePrimary,
		TaskPacket:      packet,
		ContextManifest: manifest,
		Grant:           grant,
	})
}
