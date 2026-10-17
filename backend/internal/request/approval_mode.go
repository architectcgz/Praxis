package request

import (
	"strings"

	"praxis/internal/contracts"
	"praxis/internal/core/security"
)

// SetProjectApprovalMode 保存已经校验的项目级授权选择；Mode=nil 表示继承全局。
type SetProjectApprovalMode struct {
	ProjectID contracts.ProjectID
	Mode      *security.ApprovalMode
}

// NewSetProjectApprovalMode 复制并规范化输入；未知模式或空项目身份返回校验错误。
func NewSetProjectApprovalMode(projectID contracts.ProjectID, raw *string) (SetProjectApprovalMode, error) {
	value := SetProjectApprovalMode{ProjectID: contracts.ProjectID(strings.TrimSpace(projectID.String()))}
	if value.ProjectID == "" || strings.ContainsAny(value.ProjectID.String(), "\x00\r\n") {
		return SetProjectApprovalMode{}, contracts.InvalidValue("projectId", "canonical project identity is required")
	}
	if raw != nil {
		mode := security.ApprovalMode(strings.TrimSpace(*raw))
		if !mode.ValidDefault() {
			return SetProjectApprovalMode{}, contracts.InvalidValue("approvalMode", "default approval mode must be ask, auto or yolo")
		}
		value.Mode = &mode
	}
	return value, nil
}
