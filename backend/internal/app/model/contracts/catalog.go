// Package contracts 定义模型目录和可编辑配置的公开数据，不包含凭据内容。
package contracts

// ModelOption 返回可选模型及关联的 Agent 定义。
type ModelOption struct {
	ProviderID               string
	ModelID                  string
	DefaultProviderID        string
	DefaultModelID           string
	Label                    string
	ProviderName             string
	ReasoningLevels          []string
	DefaultReasoningLevel    string
	AssignedAgentDefinitions []string
}
