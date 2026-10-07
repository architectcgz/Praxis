package model

import (
	"errors"
	"strings"
)

// ModelSnapshot 保存一次 task 创建时冻结的模型运行参数。
//
// 这些字段不能在 task 运行期间从当前模型配置重新读取，否则修改配置
// 会改变已经创建的 task 的请求协议和资源边界。
type ModelSnapshot struct {
	ProviderID      string
	ModelID         string
	ReasoningLevel  string
	APIFormat       string
	ContextWindow   int
	MaxOutputTokens int
	BaseURL         string
	ProxyURL        string
}

// Validate 校验 task 使用的模型快照是否完整且已规范化。
func (s ModelSnapshot) Validate() error {
	if s.ProviderID == "" {
		return errors.New("task model provider ID is required")
	}
	if s.ModelID == "" {
		return errors.New("task model ID is required")
	}
	if s.ProviderID != strings.TrimSpace(s.ProviderID) || s.ModelID != strings.TrimSpace(s.ModelID) ||
		s.ReasoningLevel != strings.TrimSpace(s.ReasoningLevel) {
		return errors.New("task model identity fields must be normalized")
	}
	if strings.TrimSpace(s.APIFormat) == "" || s.APIFormat != strings.TrimSpace(s.APIFormat) {
		return errors.New("task model API format is required and must be normalized")
	}
	if s.ContextWindow <= 0 || s.MaxOutputTokens <= 0 || s.MaxOutputTokens >= s.ContextWindow {
		return errors.New("task model capability limits are invalid")
	}
	if strings.TrimSpace(s.BaseURL) == "" || s.BaseURL != strings.TrimSpace(s.BaseURL) {
		return errors.New("task model base URL is required and must be normalized")
	}
	if s.ProxyURL != strings.TrimSpace(s.ProxyURL) {
		return errors.New("task model proxy URL must be normalized")
	}
	return nil
}
