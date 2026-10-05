package contracts

import (
	"praxis/internal/utils/pathutil"

	"path/filepath"
	"slices"
	"strings"
)

type ResourceLimits struct {
	MaxSteps       int
	MaxToolCalls   int
	MaxInputBytes  int64
	MaxOutputBytes int64
}

func (r ResourceLimits) Validate() error {
	if r.MaxSteps < 0 || r.MaxToolCalls < 0 || r.MaxInputBytes < 0 || r.MaxOutputBytes < 0 {
		return InvalidValue("turnPermissions.resourceLimits", "resource limits cannot be negative")
	}
	return nil
}

// TurnPermissionsSpec 描述一次 Turn 创建时冻结的权限边界。
type TurnPermissionsSpec struct {
	AllowedTools       []ToolName
	ReadScopes         []string
	WriteScopes        []string
	AllowedExecutables []string
	ResourceLimits     ResourceLimits
	WorkspaceID        WorkspaceID
	WorkspaceRevision  uint64
}

// TurnPermissions 是一次 Turn 使用的不可变权限快照，不是独立授权实体。
type TurnPermissions struct {
	AllowedTools       []ToolName
	ReadScopes         []string
	WriteScopes        []string
	AllowedExecutables []string
	ResourceLimits     ResourceLimits
	WorkspaceID        WorkspaceID
	WorkspaceRevision  uint64
}

// NewTurnPermissions 规范化并校验一次 Turn 的权限快照。
func NewTurnPermissions(spec TurnPermissionsSpec) (TurnPermissions, error) {
	permissions := TurnPermissions{
		WorkspaceID:        spec.WorkspaceID,
		WorkspaceRevision:  spec.WorkspaceRevision,
		AllowedTools:       slices.Clone(spec.AllowedTools),
		AllowedExecutables: slices.Clone(spec.AllowedExecutables),
		ReadScopes:         slices.Clone(spec.ReadScopes),
		WriteScopes:        slices.Clone(spec.WriteScopes),
		ResourceLimits:     spec.ResourceLimits,
	}
	canonicalizePermissions(&permissions)
	if err := permissions.Validate(); err != nil {
		return TurnPermissions{}, err
	}
	return permissions, nil
}

func (p TurnPermissions) Validate() error {
	if EmptyID(string(p.WorkspaceID)) {
		return InvalidValue("turnPermissions.workspaceID", "workspace id is required")
	}
	if p.WorkspaceRevision == 0 {
		return InvalidValue("turnPermissions.workspaceRevision", "workspace revision must be positive")
	}
	if err := p.ResourceLimits.Validate(); err != nil {
		return err
	}
	seenTools := make(map[ToolName]struct{}, len(p.AllowedTools))
	for _, tool := range p.AllowedTools {
		if !tool.Valid() {
			return InvalidValue("turnPermissions.allowedTools", "unknown P1 tool")
		}
		if _, exists := seenTools[tool]; exists {
			return InvalidValue("turnPermissions.allowedTools", "duplicate tool")
		}
		seenTools[tool] = struct{}{}
	}
	for _, executable := range p.AllowedExecutables {
		if executable == "" || executable != strings.TrimSpace(executable) || strings.ContainsAny(executable, "\x00\r\n") {
			return InvalidValue("turnPermissions.allowedExecutables", "executable is invalid")
		}
	}
	if containsTool(p.AllowedTools, ToolWriteFile) && len(p.WriteScopes) == 0 {
		return InvalidValue("turnPermissions.writeScopes", "write_file requires at least one write scope")
	}
	if containsTool(p.AllowedTools, ToolApplyPatch) && len(p.WriteScopes) == 0 {
		return InvalidValue("turnPermissions.writeScopes", "apply_patch requires at least one write scope")
	}
	if containsFilesystemTool(p.AllowedTools) && len(p.ReadScopes) == 0 {
		return InvalidValue("turnPermissions.readScopes", "file tools require at least one read scope")
	}
	if err := validateScopePaths(p.ReadScopes, "turnPermissions.readScopes"); err != nil {
		return err
	}
	if err := validateScopePaths(p.WriteScopes, "turnPermissions.writeScopes"); err != nil {
		return err
	}
	return nil
}

func (p TurnPermissions) AllowsTool(tool ToolName) bool {
	return containsTool(p.AllowedTools, tool)
}

func (p TurnPermissions) AllowsReadPath(path string) bool {
	return p.AllowsPath(path, p.ReadScopes)
}

func (p TurnPermissions) AllowsWritePath(path string) bool {
	return p.AllowsPath(path, p.WriteScopes)
}

func (p TurnPermissions) AllowsPath(path string, scopes []string) bool {
	if !pathutil.IsAbsoluteNormalized(path) {
		return false
	}
	for _, scope := range scopes {
		if pathutil.IsWithin(scope, path) {
			return true
		}
	}
	return false
}

func (p TurnPermissions) HasWriteAccess() bool {
	return len(p.WriteScopes) > 0
}

func canonicalizePermissions(permissions *TurnPermissions) {
	slices.Sort(permissions.AllowedTools)
	for index := range permissions.AllowedExecutables {
		permissions.AllowedExecutables[index] = strings.TrimSpace(permissions.AllowedExecutables[index])
	}
	slices.Sort(permissions.AllowedExecutables)
	permissions.ReadScopes = canonicalizeScopes(permissions.ReadScopes)
	permissions.WriteScopes = canonicalizeScopes(permissions.WriteScopes)
}

func validateScopePaths(scopes []string, field string) error {
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if !pathutil.IsAbsoluteNormalized(scope) {
			return InvalidValue(field, "scope must be an absolute normalized path")
		}
		if _, exists := seen[scope]; exists {
			return InvalidValue(field, "duplicate normalized scope")
		}
		seen[scope] = struct{}{}
	}
	return nil
}

func canonicalizeScopes(scopes []string) []string {
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed != "" {
			result = append(result, filepath.Clean(trimmed))
		}
	}
	slices.Sort(result)
	return result
}
