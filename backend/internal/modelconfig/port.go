// Package modelconfig 定义模型配置用例所需的稳定快照和端口。
package modelconfig

import "context"

type APIFormat string

type Group struct {
	ID          string
	DisplayName string
}

type Model struct {
	ID                    string
	DisplayName           string
	GroupID               string
	APIFormat             APIFormat
	ContextWindow         int
	MaxOutputTokens       int
	ReasoningLevels       []string
	DefaultReasoningLevel string
}

type Provider struct {
	ID             string
	DisplayName    string
	BaseURL        string
	ProxyURL       string
	DefaultModelID string
	Models         []Model
}

type Config struct {
	Groups            []Group
	DefaultProviderID string
	Providers         []Provider
}

type Option struct {
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

// Catalog 读取已确认的模型能力。
type Catalog interface {
	ListModels() []Option
}

// Editor 管理模型配置和 Provider 凭据，不暴露具体存储格式。
type Editor interface {
	Catalog
	Config() Config
	Save(Config) error
	SetProviderKey(string, string) error
	HasProviderKey(string) bool
	DiscoverProviderModels(context.Context, string) ([]string, error)
}

// AgentDefinitions 提供模型配置关联的 Agent 定义查询和校验。
type AgentDefinitions interface {
	DefinitionsForModel(string, string) []string
	ValidateModelConfiguration(Config) error
}

// ValidationError 表示用户可修正的配置内容错误。
type ValidationError struct {
	Err error
}

func (e *ValidationError) Error() string {
	if e == nil || e.Err == nil {
		return "模型配置无效"
	}
	return e.Err.Error()
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
