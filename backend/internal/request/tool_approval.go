package request

import (
	"praxis/internal/core/fault"
	"strings"
)

// ToolDecision 固定待审批调用的归属和参数摘要。
type ToolDecision struct {
	SessionID       string
	AgentID         string
	TaskID          string
	CallID          string
	ArgumentsDigest string
}

// NewToolDecision 规范化调用身份；缺少归属或参数摘要时返回输入错误。
func NewToolDecision(value ToolDecision) (ToolDecision, error) {
	value.CallID = strings.TrimSpace(value.CallID)
	value.ArgumentsDigest = strings.TrimSpace(value.ArgumentsDigest)
	if value.SessionID == "" || value.AgentID == "" || value.TaskID == "" || value.CallID == "" || value.ArgumentsDigest == "" {
		return ToolDecision{}, fault.New(fault.InvalidRequest, "tool approval identity is required")
	}
	return value, nil
}
