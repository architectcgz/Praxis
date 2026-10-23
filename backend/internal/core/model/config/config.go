// Package config 定义模型配置的共享数据类型和管理接口。
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
	ID                    string        `json:"id"`
	DisplayName           string        `json:"displayName"`
	GroupID               string        `json:"groupId"`
	APIFormat             APIFormat     `json:"apiFormat"`
	ContextWindow         int           `json:"contextWindow"`
	MaxOutputTokens       int           `json:"maxOutputTokens"`
	ReasoningLevels       []string      `json:"reasoningLevels,omitempty"`
	DefaultReasoningLevel string        `json:"defaultReasoningLevel,omitempty"`
	Thinking              ModelThinking `json:"thinking,omitzero"`
}

// Provider 保存模型端点和模型目录；密钥由注册表独立管理。
type Provider struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	BaseURL     string  `json:"baseUrl"`
	ProxyURL    string  `json:"proxyUrl,omitempty"`
	Models      []Model `json:"models"`
}

// Config 是模型配置的完整文档；业务层只使用输入边界已规范化的值。
type Config struct {
	Groups    []Group    `json:"groups"`
	Providers []Provider `json:"providers"`
}

// Option 是模型目录中的可选条目，携带分组与推理能力。
type Option struct {
	GroupID               string
	ProviderID            string
	ModelID               string
	Label                 string
	ProviderName          string
	ReasoningLevels       []string
	DefaultReasoningLevel string
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
	Save(Config) error
	SetProviderKey(string, string) error
	HasProviderKey(string) bool
	DiscoverProviderModels(context.Context, string) ([]string, error)
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
