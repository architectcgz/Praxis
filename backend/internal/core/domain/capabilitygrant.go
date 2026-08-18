package domain

import (
	"path/filepath"
	"sort"
	"strings"
)

type ModelRef struct {
	ID string
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
	WorkspaceKey              string
	AllowedTools              []ToolName
	ReadScopes                []string
	WriteScopes               []string
	CanProposeDelegation      bool
	ResultPermissions         []ResultPermission
	Model                     ModelRef
	ResourceLimits            ResourceLimits
	ContextManifestRef        ContextManifestID
	ApprovalSource            ApprovalSource
	ApprovalPolicyFingerprint string
}

type CapabilityGrant struct {
	ID                        CapabilityGrantID
	WorkspaceKey              string
	AllowedTools              []ToolName
	ReadScopes                []string
	WriteScopes               []string
	CanProposeDelegation      bool
	ResultPermissions         []ResultPermission
	Model                     ModelRef
	ResourceLimits            ResourceLimits
	ContextManifestRef        ContextManifestID
	ApprovalSource            ApprovalSource
	ApprovalPolicyFingerprint string
}

func NewCapabilityGrant(spec CapabilityGrantSpec) (CapabilityGrant, error) {
	grant := CapabilityGrant{
		ID:                        spec.ID,
		WorkspaceKey:              strings.TrimSpace(spec.WorkspaceKey),
		AllowedTools:              cloneTools(spec.AllowedTools),
		ReadScopes:                cloneStrings(spec.ReadScopes),
		WriteScopes:               cloneStrings(spec.WriteScopes),
		CanProposeDelegation:      spec.CanProposeDelegation,
		ResultPermissions:         cloneResultPermissions(spec.ResultPermissions),
		Model:                     ModelRef{ID: strings.TrimSpace(spec.Model.ID)},
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
	if !g.ApprovalSource.Valid() {
		return invalidValue("grant.approvalSource", "unknown approval source")
	}
	if g.ApprovalSource == ApprovalSourcePolicyDefault && g.ApprovalPolicyFingerprint == "" {
		return invalidValue("grant.approvalPolicyFingerprint", "policy approval requires a fingerprint")
	}
	if idIsEmpty(string(g.ContextManifestRef)) {
		return invalidValue("grant.contextManifestRef", "context manifest reference is required")
	}
	if g.Model.ID == "" {
		return invalidValue("grant.model.id", "model reference is required")
	}
	if err := g.ResourceLimits.Validate(); err != nil {
		return err
	}
	if g.WorkspaceKey != "" {
		cleanWorkspace := filepath.Clean(g.WorkspaceKey)
		if !filepath.IsAbs(cleanWorkspace) || cleanWorkspace != g.WorkspaceKey {
			return invalidValue("grant.workspaceKey", "workspace key must be an adapter-normalized absolute path")
		}
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
	if containsTool(g.AllowedTools, ToolSubmitResult) != containsResultPermission(g.ResultPermissions, ResultPermissionAgentResult) {
		return invalidValue("grant", "submit_result and agent_result permission must agree")
	}
	if containsTool(g.AllowedTools, ToolSubmitBriefing) != containsResultPermission(g.ResultPermissions, ResultPermissionBriefing) {
		return invalidValue("grant", "submit_briefing and briefing permission must agree")
	}
	if containsTool(g.AllowedTools, ToolWriteFile) && len(g.WriteScopes) == 0 {
		return invalidValue("grant.writeScopes", "write_file requires at least one write scope")
	}
	if len(g.WriteScopes) > 0 && !containsTool(g.AllowedTools, ToolWriteFile) {
		return invalidValue("grant.writeScopes", "write scopes require write_file")
	}
	if containsTool(g.AllowedTools, ToolRunCommand) && g.WorkspaceKey == "" {
		return invalidValue("grant.workspaceKey", "run_command requires a workspace key")
	}
	if containsFilesystemTool(g.AllowedTools) && len(g.ReadScopes) == 0 {
		return invalidValue("grant.readScopes", "file tools require at least one read scope")
	}
	if _, err := validateScopes(g.WorkspaceKey, g.ReadScopes, "grant.readScopes"); err != nil {
		return err
	}
	if _, err := validateScopes(g.WorkspaceKey, g.WriteScopes, "grant.writeScopes"); err != nil {
		return err
	}
	return nil
}

func (g CapabilityGrant) Snapshot() CapabilityGrant {
	return CapabilityGrant{
		ID:                        g.ID,
		WorkspaceKey:              g.WorkspaceKey,
		AllowedTools:              cloneTools(g.AllowedTools),
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
	if grant.WorkspaceKey != "" {
		grant.WorkspaceKey = filepath.Clean(grant.WorkspaceKey)
	}
	sort.Slice(grant.AllowedTools, func(i, j int) bool { return grant.AllowedTools[i] < grant.AllowedTools[j] })
	grant.ReadScopes = canonicalizeScopes(grant.ReadScopes)
	grant.WriteScopes = canonicalizeScopes(grant.WriteScopes)
	sort.Slice(grant.ResultPermissions, func(i, j int) bool { return grant.ResultPermissions[i] < grant.ResultPermissions[j] })
}

func validateScopes(workspaceKey string, scopes []string, field string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	if workspaceKey == "" {
		return nil, invalidValue(field, "scopes require a workspace key")
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		clean := filepath.Clean(strings.TrimSpace(scope))
		if clean == "." || !filepath.IsAbs(clean) {
			return nil, invalidValue(field, "scope must be an absolute normalized path")
		}
		if !pathWithin(workspaceKey, clean) {
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
