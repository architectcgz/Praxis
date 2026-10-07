// Package config 定义模型配置的共享数据类型、管理接口和校验错误。
package config

import (
	"context"
	"slices"
)

// APIFormat 标识 Provider 使用的模型 API 协议。
type APIFormat string

const (
	APIFormatAnthropicMessages     APIFormat = "anthropic_messages"
	APIFormatOpenAIResponses       APIFormat = "openai_responses"
	APIFormatOpenAIChatCompletions APIFormat = "openai_chat_completions"
)

// Group 定义模型目录中的分组及展示名称。
type Group struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// Model 保存可编辑的模型能力与推理等级，不包含运行时凭据。
type Model struct {
	ID                    string    `json:"id"`
	DisplayName           string    `json:"displayName"`
	GroupID               string    `json:"groupId"`
	APIFormat             APIFormat `json:"apiFormat"`
	ContextWindow         int       `json:"contextWindow"`
	MaxOutputTokens       int       `json:"maxOutputTokens"`
	ReasoningLevels       []string  `json:"reasoningLevels,omitempty"`
	DefaultReasoningLevel string    `json:"defaultReasoningLevel,omitempty"`
}

// Provider 保存模型端点和模型目录；密钥由注册表独立管理。
type Provider struct {
	ID             string  `json:"id"`
	DisplayName    string  `json:"displayName"`
	BaseURL        string  `json:"baseUrl"`
	ProxyURL       string  `json:"proxyUrl,omitempty"`
	DefaultModelID string  `json:"defaultModelId,omitempty"`
	Models         []Model `json:"models"`
}

// Config 是模型配置的完整文档；业务层只使用输入边界已规范化的值。
type Config struct {
	Groups            []Group    `json:"groups"`
	DefaultProviderID string     `json:"defaultProviderId"`
	Providers         []Provider `json:"providers"`
}

// Option 是模型目录中的可选条目，携带分组、默认选择和关联的 Agent 定义。
type Option struct {
	GroupID                  string
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

// ConfigManager 管理模型配置、Provider 凭据和模型发现，不暴露具体存储格式。
type ConfigManager interface {
	Catalog
	Config() Config
	// Save 保存输入边界已准备的配置，不再次规范化或修改配置内容。
	Save(ValidatedConfig) error
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

// Error 返回配置校验说明；没有具体原因时返回通用提示。
func (e *ValidationError) Error() string {
	if e == nil || e.Err == nil {
		return "模型配置无效"
	}
	return e.Err.Error()
}

// Unwrap 返回底层校验错误，便于调用方识别具体原因。
func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Clone 返回配置的深拷贝，调用方修改嵌套模型或推理等级不会影响原配置。
func (c Config) Clone() Config {
	c.Groups = slices.Clone(c.Groups)
	c.Providers = slices.Clone(c.Providers)
	for index := range c.Providers {
		c.Providers[index].Models = slices.Clone(c.Providers[index].Models)
		for modelIndex := range c.Providers[index].Models {
			configured := &c.Providers[index].Models[modelIndex]
			configured.ReasoningLevels = slices.Clone(configured.ReasoningLevels)
		}
	}
	return c
}

// ValidatedConfig 保存已校验的 canonical 配置，不暴露内部可变数据。
type ValidatedConfig struct {
	config   Config
	prepared bool
}

// NewValidatedConfig 复制并只读校验 canonical 配置，失败不修改输入。
// 输入层必须先完成规范化；本构造器不清洗字段或补默认值。
func NewValidatedConfig(input Config) (ValidatedConfig, error) {
	config := input.Clone()
	if err := config.Validate(); err != nil {
		return ValidatedConfig{}, err
	}
	return ValidatedConfig{config: config, prepared: true}, nil
}

// Config 返回已校验配置的独立副本。
func (c ValidatedConfig) Config() Config {
	return c.config.Clone()
}

// Prepared 判断配置是否成功构造，供存储层拒绝零值。
func (c ValidatedConfig) Prepared() bool {
	return c.prepared
}
