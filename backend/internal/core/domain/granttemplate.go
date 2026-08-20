package domain

import (
	"sort"
	"strings"
)

type ToolName string

const (
	ToolReadFile        ToolName = "read_file"
	ToolListDir         ToolName = "list_dir"
	ToolSearchText      ToolName = "search_text"
	ToolWriteFile       ToolName = "write_file"
	ToolRunCommand      ToolName = "run_command"
	ToolProposeDelegate ToolName = "propose_delegate"
	ToolSubmitResult    ToolName = "submit_result"
	ToolSubmitBriefing  ToolName = "submit_briefing"
)

func (t ToolName) Valid() bool {
	switch t {
	case ToolReadFile,
		ToolListDir,
		ToolSearchText,
		ToolWriteFile,
		ToolRunCommand,
		ToolProposeDelegate,
		ToolSubmitResult,
		ToolSubmitBriefing:
		return true
	default:
		return false
	}
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

type ResultPermission string

const (
	ResultPermissionAgentResult ResultPermission = "agent_result"
	ResultPermissionBriefing    ResultPermission = "briefing"
	ResultPermissionNote        ResultPermission = "note"
)

func (p ResultPermission) Valid() bool {
	switch p {
	case ResultPermissionAgentResult, ResultPermissionBriefing, ResultPermissionNote:
		return true
	default:
		return false
	}
}

type DefaultGrantTemplate struct {
	AllowedTools      []ToolName
	WorkspaceAccess   WorkspaceAccess
	ResultPermissions []ResultPermission
}

func NewDefaultGrantTemplate(
	tools []ToolName,
	workspaceAccess WorkspaceAccess,
	resultPermissions []ResultPermission,
) (DefaultGrantTemplate, error) {
	template := DefaultGrantTemplate{
		AllowedTools:      cloneTools(tools),
		WorkspaceAccess:   workspaceAccess,
		ResultPermissions: cloneResultPermissions(resultPermissions),
	}
	canonicalizeTemplate(&template)
	if err := template.Validate(); err != nil {
		return DefaultGrantTemplate{}, err
	}
	return template, nil
}

func (t DefaultGrantTemplate) Validate() error {
	if !t.WorkspaceAccess.Valid() {
		return invalidValue("grantTemplate.workspaceAccess", "unknown workspace access selector")
	}
	seenTools := make(map[ToolName]struct{}, len(t.AllowedTools))
	for _, tool := range t.AllowedTools {
		if !tool.Valid() {
			return invalidValue("grantTemplate.allowedTools", "unknown P1 tool")
		}
		if _, exists := seenTools[tool]; exists {
			return invalidValue("grantTemplate.allowedTools", "duplicate tool")
		}
		seenTools[tool] = struct{}{}
	}
	seenResults := make(map[ResultPermission]struct{}, len(t.ResultPermissions))
	for _, permission := range t.ResultPermissions {
		if !permission.Valid() {
			return invalidValue("grantTemplate.resultPermissions", "unknown result permission")
		}
		if _, exists := seenResults[permission]; exists {
			return invalidValue("grantTemplate.resultPermissions", "duplicate result permission")
		}
		seenResults[permission] = struct{}{}
	}
	if t.WorkspaceAccess == WorkspaceAccessNone && containsFilesystemTool(t.AllowedTools) {
		return invalidValue("grantTemplate.workspaceAccess", "file tools require workspace access")
	}
	if t.WorkspaceAccess == WorkspaceAccessRead && containsTool(t.AllowedTools, ToolWriteFile) {
		return invalidValue("grantTemplate.workspaceAccess", "write_file requires read_write access")
	}
	if containsTool(
		t.AllowedTools,
		ToolSubmitResult,
	) != containsResultPermission(
		t.ResultPermissions,
		ResultPermissionAgentResult,
	) {
		return invalidValue("grantTemplate", "submit_result and agent_result permission must agree")
	}
	if containsTool(
		t.AllowedTools,
		ToolSubmitBriefing,
	) != containsResultPermission(
		t.ResultPermissions,
		ResultPermissionBriefing,
	) {
		return invalidValue("grantTemplate", "submit_briefing and briefing permission must agree")
	}
	return nil
}

func (t DefaultGrantTemplate) AllowsTool(tool ToolName) bool {
	return containsTool(t.AllowedTools, tool)
}

func (t DefaultGrantTemplate) AllowsResult(permission ResultPermission) bool {
	return containsResultPermission(t.ResultPermissions, permission)
}

func (t DefaultGrantTemplate) Snapshot() DefaultGrantTemplate {
	return DefaultGrantTemplate{
		AllowedTools:      cloneTools(t.AllowedTools),
		WorkspaceAccess:   t.WorkspaceAccess,
		ResultPermissions: cloneResultPermissions(t.ResultPermissions),
	}
}

func canonicalizeTemplate(template *DefaultGrantTemplate) {
	sort.Slice(
		template.AllowedTools,
		func(i, j int) bool { return template.AllowedTools[i] < template.AllowedTools[j] },
	)
	sort.Slice(
		template.ResultPermissions,
		func(i, j int) bool { return template.ResultPermissions[i] < template.ResultPermissions[j] },
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
		containsTool(tools, ToolWriteFile)
}

func containsResultPermission(permissions []ResultPermission, target ResultPermission) bool {
	for _, permission := range permissions {
		if permission == target {
			return true
		}
	}
	return false
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneTools(values []ToolName) []ToolName {
	if values == nil {
		return nil
	}
	return append([]ToolName(nil), values...)
}

func cloneResultPermissions(values []ResultPermission) []ResultPermission {
	if values == nil {
		return nil
	}
	return append([]ResultPermission(nil), values...)
}

func cloneContentRefs(values []ContentRef) []ContentRef {
	if values == nil {
		return nil
	}
	return append([]ContentRef(nil), values...)
}

func fmtField(field string, err error) error {
	if err == nil {
		return nil
	}
	return invalidValue(field, strings.TrimPrefix(err.Error(), field+": "))
}
