package run

import (
	modelapi "praxis/internal/core/model"
	"praxis/internal/identity"
)

// RunConfig 持有所选的只读会话配置，模型能力列表共享底层数据。
type RunConfig struct {
	AgentDefinitionID identity.AgentDefinitionID
	SystemPrompt      string
	Model             modelapi.Config
}
