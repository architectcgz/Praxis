package request

import (
	"strings"

	"praxis/internal/core/fault"
	"praxis/internal/core/security"
)

// SetProjectApprovalMode 保存已经校验的项目级授权选择；Mode=nil 表示继承全局。
type SetProjectApprovalMode struct {
	ProjectID string
	Mode      *security.ApprovalMode
}

// NewSetProjectApprovalMode 复制并规范化输入；未知模式或空项目身份返回校验错误。
func NewSetProjectApprovalMode(projectID string, raw *string) (SetProjectApprovalMode, error) {
	value := SetProjectApprovalMode{ProjectID: strings.TrimSpace(projectID)}
	if value.ProjectID == "" || strings.ContainsAny(value.ProjectID, "\x00\r\n") {
		return SetProjectApprovalMode{}, fault.InvalidValue("projectId", "canonical project identity is required")
	}
	if raw != nil {
		mode := security.ApprovalMode(strings.TrimSpace(*raw))
		if !mode.ValidDefault() {
			return SetProjectApprovalMode{}, fault.InvalidValue("approvalMode", "default approval mode must be ask, auto or yolo")
		}
		value.Mode = &mode
	}
	return value, nil
}
