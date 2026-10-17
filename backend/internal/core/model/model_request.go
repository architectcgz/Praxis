package model

import (
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
)

// ModelRequest 是发送给 Provider 的一次完整模型上下文请求，不包含凭据字段。
type ModelRequest struct {
	TaskID contracts.TaskID
	// PromptCacheKey 用于 Provider 的 Prompt Cache 路由，留空时不发送。
	PromptCacheKey  string
	Context         appcontext.ModelContext
	Model           Config
	MaxOutputTokens int
	Tools           []contracts.ToolDefinition
	TurnID          contracts.TurnID
}
