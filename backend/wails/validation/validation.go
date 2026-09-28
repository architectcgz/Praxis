// Package validation 在 wails 边界做入参形状校验：必填、trim、空值、请求内引用完整性和分页上限。
//
// 只校验请求本身的形状；依赖当前状态或持久化数据的规则属于用例/领域层。
// 校验失败返回 Error，携带稳定码 invalid_request 与具体字段/原因。
package validation

import (
	"fmt"
	"strings"

	"praxis/wails/dto"
	apperr "praxis/wails/error"
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

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func requireID(field, value string) error {
	if blank(value) {
		return invalid(field, "required")
	}
	return nil
}

// ValidateAgentID 校验 agentId 必填。
func ValidateAgentID(value string) error { return requireID("agentId", value) }

// ValidateSessionID 校验 sessionId 必填。
func ValidateSessionID(value string) error { return requireID("sessionId", value) }

// ValidateExecutionID 校验 executionId 必填。
func ValidateExecutionID(value string) error { return requireID("executionId", value) }

// ValidateProjectID 校验 projectId 必填。
func ValidateProjectID(value string) error { return requireID("projectId", value) }

// ValidateProviderID 校验 providerId 必填。
func ValidateProviderID(value string) error { return requireID("providerId", value) }

// ValidateSendInput 校验用户输入请求：请求 ID、内容、以及 session/agent 至少一个。
func ValidateSendInput(request dto.SendInputRequest) error {
	if err := requireID("requestId", request.RequestID); err != nil {
		return err
	}
	if blank(request.Content) {
		return invalid("content", "required")
	}
	if blank(request.SessionID) && blank(request.AgentID) {
		return invalid("sessionId", "sessionId or agentId is required")
	}
	return nil
}

// ValidateResume 校验恢复请求的必填字段。
//
// content 必填：core 的 AgentExecution 只对 user_input 强制要求起始内容，
// resume 原因可以带着空内容落库，因此这里必须在边界拦住。
func ValidateResume(request dto.ResumeRequest) error {
	if err := requireID("agentId", request.AgentID); err != nil {
		return err
	}
	if err := requireID("requestId", request.RequestID); err != nil {
		return err
	}
	if blank(request.Content) {
		return invalid("content", "required")
	}
	return nil
}

// ValidateAgentControl 校验暂停/关闭等控制命令的必填字段。
func ValidateAgentControl(request dto.PauseAgentRequest) error {
	if err := requireID("commandId", request.CommandID); err != nil {
		return err
	}
	return requireID("agentId", request.AgentID)
}

// ValidateQueueWork 校验入队请求的必填字段。
func ValidateQueueWork(request dto.QueueWorkRequest) error {
	if err := requireID("id", request.ID); err != nil {
		return err
	}
	if err := requireID("requestId", request.RequestID); err != nil {
		return err
	}
	if err := requireID("agentId", request.AgentID); err != nil {
		return err
	}
	if blank(request.Prompt) {
		return invalid("prompt", "required")
	}
	return nil
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

// ValidateSaveModelConfig 校验模型配置文档内部的引用完整性。
//
// 必须在这里拦：binding 组装 RegistryConfig 时只把 model 挂到同请求内已声明的
// provider 下，找不到 provider 的 model 会被静默丢弃，core 校验的是丢弃之后的
// 配置，无法发现丢失。
func ValidateSaveModelConfig(request dto.SaveModelConfigRequest) error {
	providers := make(map[string]struct{}, len(request.Providers))
	for _, provider := range request.Providers {
		providers[strings.TrimSpace(provider.ID)] = struct{}{}
	}
	for index, option := range request.Models {
		providerID := strings.TrimSpace(option.ProviderID)
		if providerID == "" {
			return invalid(fmt.Sprintf("models[%d].providerId", index), "required")
		}
		if _, ok := providers[providerID]; !ok {
			return invalid(fmt.Sprintf("models[%d].providerId", index), "unknown provider "+providerID)
		}
	}
	return nil
}

// maxPageLimit 是 wails 边界允许的最大单页条数。
// core 的 targetLimit 只把非正值兜底为默认页大小，不设上限，所以上限只能在边界拦。
const maxPageLimit = 500

// ValidatePageLimit 校验分页请求的条数上限。
func ValidatePageLimit(limit int) error {
	if limit > maxPageLimit {
		return invalid("limit", fmt.Sprintf("must not exceed %d", maxPageLimit))
	}
	return nil
}
