package contracts

import (
	"sort"
	"strings"
)

type ToolName string

const (
	ToolReadFile   ToolName = "read_file"
	ToolListDir    ToolName = "list_dir"
	ToolSearchText ToolName = "search_text"
	ToolWriteFile  ToolName = "write_file"
	ToolRunCommand ToolName = "run_command"
	ToolBash       ToolName = "bash"
)

func (t ToolName) Valid() bool {
	if t == "" || len(t) > 64 || strings.HasPrefix(string(t), "_") {
		return false
	}
	for _, char := range t {
		if char != '_' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

type WorkspaceAccess string

const (
	WorkspaceAccessNone      WorkspaceAccess = "none"
	WorkspaceAccessRead      WorkspaceAccess = "read"
	WorkspaceAccessReadWrite WorkspaceAccess = "read_write"
)

func (a WorkspaceAccess) Valid() bool {
	switch a {
	case WorkspaceAccessNone, WorkspaceAccessRead, WorkspaceAccessReadWrite:
		return true
	default:
		return false
	}
}

type DefaultGrantTemplate struct {
	AllowedTools    []ToolName
	WorkspaceAccess WorkspaceAccess
}

func NewDefaultGrantTemplate(
	tools []ToolName,
	workspaceAccess WorkspaceAccess,
) (DefaultGrantTemplate, error) {
	template := DefaultGrantTemplate{
		AllowedTools:    cloneTools(tools),
		WorkspaceAccess: workspaceAccess,
	}
	canonicalizeTemplate(&template)
	if err := template.Validate(); err != nil {
		return DefaultGrantTemplate{}, err
	}
	return template, nil
}

func (t DefaultGrantTemplate) Validate() error {
	if !t.WorkspaceAccess.Valid() {
		return InvalidValue("grantTemplate.workspaceAccess", "unknown workspace access selector")
	}
	seenTools := make(map[ToolName]struct{}, len(t.AllowedTools))
	for _, tool := range t.AllowedTools {
		if !tool.Valid() {
			return InvalidValue("grantTemplate.allowedTools", "unknown P1 tool")
		}
		if _, exists := seenTools[tool]; exists {
			return InvalidValue("grantTemplate.allowedTools", "duplicate tool")
		}
		seenTools[tool] = struct{}{}
	}
	if t.WorkspaceAccess == WorkspaceAccessNone && containsFilesystemTool(t.AllowedTools) {
		return InvalidValue("grantTemplate.workspaceAccess", "file tools require workspace access")
	}
	if t.WorkspaceAccess == WorkspaceAccessRead && containsTool(t.AllowedTools, ToolWriteFile) {
		return InvalidValue("grantTemplate.workspaceAccess", "write_file requires read_write access")
	}
	return nil
}

func (t DefaultGrantTemplate) AllowsTool(tool ToolName) bool {
	return containsTool(t.AllowedTools, tool)
}

func (t DefaultGrantTemplate) Snapshot() DefaultGrantTemplate {
	return DefaultGrantTemplate{
		AllowedTools:    cloneTools(t.AllowedTools),
		WorkspaceAccess: t.WorkspaceAccess,
	}
}

func canonicalizeTemplate(template *DefaultGrantTemplate) {
	sort.Slice(
		template.AllowedTools,
		func(i, j int) bool { return template.AllowedTools[i] < template.AllowedTools[j] },
	)
}

func containsTool(tools []ToolName, target ToolName) bool {
	for _, tool := range tools {
		if tool == target {
			return true
		}
	}
	return false
}

func containsFilesystemTool(tools []ToolName) bool {
	return containsTool(tools, ToolReadFile) || containsTool(tools, ToolListDir) ||
		containsTool(tools, ToolSearchText) ||
		containsTool(tools, ToolWriteFile) || containsTool(tools, ToolBash)
}

func cloneTools(values []ToolName) []ToolName {
	if values == nil {
		return nil
	}
	return append([]ToolName(nil), values...)
}
