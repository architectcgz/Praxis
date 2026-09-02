package security

import (
	"path/filepath"
	"sort"
	"strings"
)

type ModelSelection struct {
	ProviderID string
	ModelID    string
	Reasoning  string
}

type ResourceLimits struct {
	MaxTurns       int
	MaxToolCalls   int
	MaxInputBytes  int64
	MaxOutputBytes int64
}

func (r ResourceLimits) Validate() error {
	if r.MaxTurns < 0 || r.MaxToolCalls < 0 || r.MaxInputBytes < 0 || r.MaxOutputBytes < 0 {
		return invalidValue("grant.resourceLimits", "resource limits cannot be negative")
	}
	return nil
}

type CapabilityGrantSpec struct {
	ID                        CapabilityGrantID
	WorkspaceID               WorkspaceID
	WorkspacePathSnapshot     string
	WorkspaceRevision         uint64
	AllowedTools              []ToolName
	AllowedExecutables        []string
	ReadScopes                []string
	WriteScopes               []string
	CanProposeDelegation      bool
	ResultPermissions         []ResultPermission
	Model                     ModelSelection
	ResourceLimits            ResourceLimits
	ContextManifestRef        ContextManifestID
	ApprovalSource            ApprovalSource
	ApprovalPolicyFingerprint string
}

type CapabilityGrant struct {
	ID                        CapabilityGrantID
	WorkspaceID               WorkspaceID
	WorkspacePathSnapshot     string
	WorkspaceRevision         uint64
	AllowedTools              []ToolName
	AllowedExecutables        []string
	ReadScopes                []string
	WriteScopes               []string
	CanProposeDelegation      bool
	ResultPermissions         []ResultPermission
	Model                     ModelSelection
	ResourceLimits            ResourceLimits
	ContextManifestRef        ContextManifestID
	ApprovalSource            ApprovalSource
	ApprovalPolicyFingerprint string
}

func NewCapabilityGrant(spec CapabilityGrantSpec) (CapabilityGrant, error) {
	grant := CapabilityGrant{
		ID:                    spec.ID,
		WorkspaceID:           spec.WorkspaceID,
		WorkspacePathSnapshot: strings.TrimSpace(spec.WorkspacePathSnapshot),
		WorkspaceRevision:     spec.WorkspaceRevision,
		AllowedTools:          cloneTools(spec.AllowedTools),
		AllowedExecutables:    cloneStrings(spec.AllowedExecutables),
		ReadScopes:            cloneStrings(spec.ReadScopes),
		WriteScopes:           cloneStrings(spec.WriteScopes),
		CanProposeDelegation:  spec.CanProposeDelegation,
		ResultPermissions:     cloneResultPermissions(spec.ResultPermissions),
		Model: ModelSelection{
			ProviderID: strings.TrimSpace(spec.Model.ProviderID),
			ModelID:    strings.TrimSpace(spec.Model.ModelID),
			Reasoning:  strings.TrimSpace(spec.Model.Reasoning),
		},
		ResourceLimits:            spec.ResourceLimits,
		ContextManifestRef:        spec.ContextManifestRef,
		ApprovalSource:            spec.ApprovalSource,
		ApprovalPolicyFingerprint: strings.TrimSpace(spec.ApprovalPolicyFingerprint),
	}
	canonicalizeGrant(&grant)
	if err := grant.Validate(); err != nil {
		return CapabilityGrant{}, err
	}
	return grant, nil
}

func (g CapabilityGrant) Validate() error {
	if idIsEmpty(string(g.ID)) {
		return invalidValue("grant.id", "id is required")
	}
	if idIsEmpty(string(g.WorkspaceID)) {
		return invalidValue("grant.workspaceID", "workspace id is required")
	}
	if g.WorkspacePathSnapshot == "" {
		return invalidValue("grant.workspacePathSnapshot", "workspace path snapshot is required")
	}
	cleanPath := filepath.Clean(g.WorkspacePathSnapshot)
	if !filepath.IsAbs(cleanPath) || cleanPath != g.WorkspacePathSnapshot {
		return invalidValue("grant.workspacePathSnapshot", "workspace path snapshot must be an absolute normalized path")
	}
	if g.WorkspaceRevision == 0 {
		return invalidValue("grant.workspaceRevision", "workspace revision must be positive")
	}
	if !g.ApprovalSource.Valid() {
		return invalidValue("grant.approvalSource", "unknown approval source")
	}
	if g.ApprovalSource == ApprovalSourcePolicyDefault && g.ApprovalPolicyFingerprint == "" {
		return invalidValue("grant.approvalPolicyFingerprint", "policy approval requires a fingerprint")
	}
	if idIsEmpty(string(g.ContextManifestRef)) {
		return invalidValue("grant.contextManifestRef", "context manifest reference is required")
	}
	if g.Model.ProviderID == "" || g.Model.ModelID == "" {
		return invalidValue("grant.model", "provider and model IDs are required")
	}
	if err := g.ResourceLimits.Validate(); err != nil {
		return err
	}
	seenTools := make(map[ToolName]struct{}, len(g.AllowedTools))
	for _, tool := range g.AllowedTools {
		if !tool.Valid() {
			return invalidValue("grant.allowedTools", "unknown P1 tool")
		}
		if _, exists := seenTools[tool]; exists {
			return invalidValue("grant.allowedTools", "duplicate tool")
		}
		seenTools[tool] = struct{}{}
	}
	for _, executable := range g.AllowedExecutables {
		if strings.TrimSpace(executable) == "" || strings.ContainsAny(executable, "\x00\r\n") {
			return invalidValue("grant.allowedExecutables", "executable is invalid")
		}
	}
	seenResults := make(map[ResultPermission]struct{}, len(g.ResultPermissions))
	for _, permission := range g.ResultPermissions {
		if !permission.Valid() {
			return invalidValue("grant.resultPermissions", "unknown result permission")
		}
		if _, exists := seenResults[permission]; exists {
			return invalidValue("grant.resultPermissions", "duplicate result permission")
		}
		seenResults[permission] = struct{}{}
	}
	if g.CanProposeDelegation != containsTool(g.AllowedTools, ToolProposeDelegate) {
		return invalidValue("grant.canProposeDelegation", "delegation flag and tool permission must agree")
	}
	if containsTool(
		g.AllowedTools,
		ToolSubmitResult,
	) != containsResultPermission(
		g.ResultPermissions,
		ResultPermissionAgentResult,
	) {
		return invalidValue("grant", "submit_result and agent_result permission must agree")
	}
	if containsTool(
		g.AllowedTools,
		ToolSubmitBriefing,
	) != containsResultPermission(
		g.ResultPermissions,
		ResultPermissionBriefing,
	) {
		return invalidValue("grant", "submit_briefing and briefing permission must agree")
	}
	if containsTool(g.AllowedTools, ToolWriteFile) && len(g.WriteScopes) == 0 {
		return invalidValue("grant.writeScopes", "write_file requires at least one write scope")
	}
	if len(g.WriteScopes) > 0 && !containsTool(g.AllowedTools, ToolWriteFile) {
		return invalidValue("grant.writeScopes", "write scopes require write_file")
	}
	if containsFilesystemTool(g.AllowedTools) && len(g.ReadScopes) == 0 {
		return invalidValue("grant.readScopes", "file tools require at least one read scope")
	}
	if _, err := validateScopes(g.WorkspacePathSnapshot, g.ReadScopes, "grant.readScopes"); err != nil {
		return err
	}
	if _, err := validateScopes(g.WorkspacePathSnapshot, g.WriteScopes, "grant.writeScopes"); err != nil {
		return err
	}
	return nil
}

func (g CapabilityGrant) Snapshot() CapabilityGrant {
	return CapabilityGrant{
		ID:                        g.ID,
		WorkspaceID:               g.WorkspaceID,
		WorkspacePathSnapshot:     g.WorkspacePathSnapshot,
		WorkspaceRevision:         g.WorkspaceRevision,
		AllowedTools:              cloneTools(g.AllowedTools),
		AllowedExecutables:        cloneStrings(g.AllowedExecutables),
		ReadScopes:                cloneStrings(g.ReadScopes),
		WriteScopes:               cloneStrings(g.WriteScopes),
		CanProposeDelegation:      g.CanProposeDelegation,
		ResultPermissions:         cloneResultPermissions(g.ResultPermissions),
		Model:                     g.Model,
		ResourceLimits:            g.ResourceLimits,
		ContextManifestRef:        g.ContextManifestRef,
		ApprovalSource:            g.ApprovalSource,
		ApprovalPolicyFingerprint: g.ApprovalPolicyFingerprint,
	}
}

func (g CapabilityGrant) AllowsTool(tool ToolName) bool { return containsTool(g.AllowedTools, tool) }

func (g CapabilityGrant) AllowsResult(permission ResultPermission) bool {
	return containsResultPermission(g.ResultPermissions, permission)
}

func (g CapabilityGrant) AllowsReadPath(path string) bool {
	return g.AllowsPath(path, g.ReadScopes)
}

func (g CapabilityGrant) AllowsWritePath(path string) bool {
	return g.AllowsTool(ToolWriteFile) && g.AllowsPath(path, g.WriteScopes)
}

func (g CapabilityGrant) AllowsPath(path string, scopes []string) bool {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "." || !filepath.IsAbs(cleanPath) {
		return false
	}
	for _, scope := range scopes {
		if pathWithin(scope, cleanPath) {
			return true
		}
	}
	return false
}

func (g CapabilityGrant) HasWriteAccess() bool {
	return g.AllowsTool(ToolWriteFile) && len(g.WriteScopes) > 0
}

func canonicalizeGrant(grant *CapabilityGrant) {
	if grant.WorkspacePathSnapshot != "" {
		grant.WorkspacePathSnapshot = filepath.Clean(grant.WorkspacePathSnapshot)
	}
	sort.Slice(grant.AllowedTools, func(i, j int) bool { return grant.AllowedTools[i] < grant.AllowedTools[j] })
	for index := range grant.AllowedExecutables {
		grant.AllowedExecutables[index] = strings.TrimSpace(grant.AllowedExecutables[index])
	}
	sort.Strings(grant.AllowedExecutables)
	grant.ReadScopes = canonicalizeScopes(grant.ReadScopes)
	grant.WriteScopes = canonicalizeScopes(grant.WriteScopes)
	sort.Slice(
		grant.ResultPermissions,
		func(i, j int) bool { return grant.ResultPermissions[i] < grant.ResultPermissions[j] },
	)
}

func validateScopes(workspacePath string, scopes []string, field string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	if workspacePath == "" {
		return nil, invalidValue(field, "scopes require a workspace path")
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		clean := filepath.Clean(strings.TrimSpace(scope))
		if clean == "." || !filepath.IsAbs(clean) {
			return nil, invalidValue(field, "scope must be an absolute normalized path")
		}
		if !pathWithin(workspacePath, clean) {
			return nil, invalidValue(field, "scope must remain inside the workspace")
		}
		if _, exists := seen[clean]; exists {
			return nil, invalidValue(field, "duplicate normalized scope")
		}
		seen[clean] = struct{}{}
	}
	return canonicalizeScopes(scopes), nil
}

func canonicalizeScopes(scopes []string) []string {
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed != "" {
			result = append(result, filepath.Clean(trimmed))
		}
	}
	sort.Strings(result)
	return result
}

func uniqueTools(tools []ToolName) []ToolName {
	result := make([]ToolName, 0, len(tools))
	seen := make(map[ToolName]struct{}, len(tools))
	for _, tool := range tools {
		if _, exists := seen[tool]; !exists {
			seen[tool] = struct{}{}
			result = append(result, tool)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func uniqueResultPermissions(values []ResultPermission) []ResultPermission {
	result := make([]ResultPermission, 0, len(values))
	seen := make(map[ResultPermission]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
