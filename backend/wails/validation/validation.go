// Package validation 在 Wails 边界校验请求 ID 等 wire shape。
//
// 内容规范化、大小限制和请求字段校验由对应 Binding 完成。
// 校验失败返回 Error，携带稳定码 invalid_request 与具体字段/原因。
package validation

import (
	"praxis/wails/dto"
	apperr "praxis/wails/error"
	"strings"
)

// Error 是入参校验错误：稳定码 + 字段 + 原因。
type Error struct {
	Field  string
	Reason string
}

// ErrorCode 让 app 层把校验错误归类为 invalid_request。
func (e *Error) ErrorCode() string { return string(apperr.ErrorCodeInvalidRequest) }

// Error 返回 wire 信封 {"code","message"}，message 为具体字段/原因。
func (e *Error) Error() string {
	return apperr.Encode(e.ErrorCode(), e.detail())
}

func (e *Error) detail() string {
	switch {
	case e.Field != "" && e.Reason != "":
		return e.Field + ": " + e.Reason
	case e.Field != "":
		return e.Field
	default:
		return e.Reason
	}
}

func invalid(field, reason string) error {
	return &Error{Field: field, Reason: reason}
}

func requireID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return invalid(field, "required")
	}
	if value != trimmed {
		return invalid(field, "must be normalized")
	}
	return nil
}

// ValidateAgentID 校验 agentId 必填。
func ValidateAgentID(value string) error { return requireID("agentId", value) }

// ValidateTaskID 校验 taskId 必填且已规范化。
func ValidateTaskID(value string) error { return requireID("taskId", value) }

// ValidateSessionID 校验 sessionId 必填。
func ValidateSessionID(value string) error { return requireID("sessionId", value) }

// ValidateProjectID 校验 projectId 必填。
func ValidateProjectID(value string) error { return requireID("projectId", value) }

// ValidateProviderID 校验已规范化的 providerId 必填。
func ValidateProviderID(value string) error {
	return requireID("providerId", value)
}

// ValidateAgentControl 校验暂停或取消请求的 commandId、agentId、targetTaskId 必填且已规范化。
func ValidateAgentControl(request dto.PauseAgentRequest) error {
	if err := requireID("commandId", request.CommandID); err != nil {
		return err
	}
	if err := requireID("agentId", request.AgentID); err != nil {
		return err
	}
	return requireID("targetTaskId", request.TargetExecutionID)
}

// ValidateCreateProject 校验建项目请求的必填字段。
func ValidateCreateProject(request dto.CreateProjectRequest) error {
	if err := requireID("projectId", request.ProjectID); err != nil {
		return err
	}
	if err := requireID("workspaceId", request.WorkspaceID); err != nil {
		return err
	}
	return requireID("requestId", request.RequestID)
}

// ValidateCreateSession 校验建会话请求的必填字段。
func ValidateCreateSession(request dto.CreateSessionRequest) error {
	if err := requireID("sessionId", request.SessionID); err != nil {
		return err
	}
	if err := requireID("agentId", request.AgentID); err != nil {
		return err
	}
	if err := requireID("projectId", request.ProjectID); err != nil {
		return err
	}
	if err := requireID("workspaceId", request.WorkspaceID); err != nil {
		return err
	}
	if err := requireID("agentDefinitionId", request.AgentDefinitionID); err != nil {
		return err
	}
	return requireID("requestId", request.RequestID)
}
