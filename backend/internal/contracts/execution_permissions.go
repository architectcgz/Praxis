package contracts

import (
	"path/filepath"
	"slices"
	"strings"
)

type ResourceLimits struct {
	MaxTurns       int
	MaxToolCalls   int
	MaxInputBytes  int64
	MaxOutputBytes int64
}

func (r ResourceLimits) Validate() error {
	if r.MaxTurns < 0 || r.MaxToolCalls < 0 || r.MaxInputBytes < 0 || r.MaxOutputBytes < 0 {
		return InvalidValue("executionPermissions.resourceLimits", "resource limits cannot be negative")
	}
	return nil
}

// ExecutionPermissionsSpec 描述一次 Execution 创建时冻结的权限边界。
type ExecutionPermissionsSpec struct {
	AllowedTools       []ToolName
	ReadScopes         []string
	WriteScopes        []string
	AllowedExecutables []string
	ResourceLimits     ResourceLimits
	WorkspaceID        WorkspaceID
	WorkspaceRevision  uint64
}

// ExecutionPermissions 是一次 Execution 使用的不可变权限快照，不是独立授权实体。
type ExecutionPermissions struct {
	AllowedTools       []ToolName
	ReadScopes         []string
	WriteScopes        []string
	AllowedExecutables []string
	ResourceLimits     ResourceLimits
	WorkspaceID        WorkspaceID
	WorkspaceRevision  uint64
}

// NewExecutionPermissions 规范化并校验一次 Execution 的权限快照。
func NewExecutionPermissions(spec ExecutionPermissionsSpec) (ExecutionPermissions, error) {
	permissions := ExecutionPermissions{
		WorkspaceID:        spec.WorkspaceID,
		WorkspaceRevision:  spec.WorkspaceRevision,
		AllowedTools:       cloneTools(spec.AllowedTools),
		AllowedExecutables: cloneStrings(spec.AllowedExecutables),
		ReadScopes:         cloneStrings(spec.ReadScopes),
		WriteScopes:        cloneStrings(spec.WriteScopes),
		ResourceLimits:     spec.ResourceLimits,
	}
	canonicalizePermissions(&permissions)
	if err := permissions.Validate(); err != nil {
		return ExecutionPermissions{}, err
	}
	return permissions, nil
}

func (p ExecutionPermissions) Validate() error {
	if EmptyID(string(p.WorkspaceID)) {
		return InvalidValue("executionPermissions.workspaceID", "workspace id is required")
	}
	if p.WorkspaceRevision == 0 {
		return InvalidValue("executionPermissions.workspaceRevision", "workspace revision must be positive")
	}
	if err := p.ResourceLimits.Validate(); err != nil {
		return err
	}
	seenTools := make(map[ToolName]struct{}, len(p.AllowedTools))
	for _, tool := range p.AllowedTools {
		if !tool.Valid() {
			return InvalidValue("executionPermissions.allowedTools", "unknown P1 tool")
		}
		if _, exists := seenTools[tool]; exists {
			return InvalidValue("executionPermissions.allowedTools", "duplicate tool")
		}
		seenTools[tool] = struct{}{}
	}
	for _, executable := range p.AllowedExecutables {
		if strings.TrimSpace(executable) == "" || strings.ContainsAny(executable, "\x00\r\n") {
			return InvalidValue("executionPermissions.allowedExecutables", "executable is invalid")
		}
	}
	if containsTool(p.AllowedTools, ToolWriteFile) && len(p.WriteScopes) == 0 {
		return InvalidValue("executionPermissions.writeScopes", "write_file requires at least one write scope")
	}
	if containsFilesystemTool(p.AllowedTools) && len(p.ReadScopes) == 0 {
		return InvalidValue("executionPermissions.readScopes", "file tools require at least one read scope")
	}
	if err := validateScopePaths(p.ReadScopes, "executionPermissions.readScopes"); err != nil {
		return err
	}
	if err := validateScopePaths(p.WriteScopes, "executionPermissions.writeScopes"); err != nil {
		return err
	}
	return nil
}

// Snapshot 返回权限快照的独立副本，避免修改切片影响已冻结的 Execution 输入。
func (p ExecutionPermissions) Snapshot() ExecutionPermissions {
	return ExecutionPermissions{
		WorkspaceID:        p.WorkspaceID,
		WorkspaceRevision:  p.WorkspaceRevision,
		AllowedTools:       cloneTools(p.AllowedTools),
		AllowedExecutables: cloneStrings(p.AllowedExecutables),
		ReadScopes:         cloneStrings(p.ReadScopes),
		WriteScopes:        cloneStrings(p.WriteScopes),
		ResourceLimits:     p.ResourceLimits,
	}
}

func (p ExecutionPermissions) AllowsTool(tool ToolName) bool {
	return containsTool(p.AllowedTools, tool)
}

func (p ExecutionPermissions) AllowsReadPath(path string) bool {
	return p.AllowsPath(path, p.ReadScopes)
}

func (p ExecutionPermissions) AllowsWritePath(path string) bool {
	return p.AllowsPath(path, p.WriteScopes)
}

func (p ExecutionPermissions) AllowsPath(path string, scopes []string) bool {
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

func (p ExecutionPermissions) HasWriteAccess() bool {
	return len(p.WriteScopes) > 0
}

func canonicalizePermissions(permissions *ExecutionPermissions) {
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
		clean := filepath.Clean(strings.TrimSpace(scope))
		if clean == "." || !filepath.IsAbs(clean) {
			return InvalidValue(field, "scope must be an absolute normalized path")
		}
		if _, exists := seen[clean]; exists {
			return InvalidValue(field, "duplicate normalized scope")
		}
		seen[clean] = struct{}{}
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

func uniqueTools(tools []ToolName) []ToolName {
	result := make([]ToolName, 0, len(tools))
	seen := make(map[ToolName]struct{}, len(tools))
	for _, tool := range tools {
		if _, exists := seen[tool]; !exists {
			seen[tool] = struct{}{}
			result = append(result, tool)
		}
	}
	slices.Sort(result)
	return result
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
