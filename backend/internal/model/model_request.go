package model

import (
	"encoding/json"
	"errors"
	"fmt"
	appcontext "praxis/internal/context"
	"praxis/internal/identity"
	"praxis/internal/tool"
	"strings"
)

// ModelRequest 是发送给 Provider 的一次完整模型上下文请求，不包含凭据字段。
type ModelRequest struct {
	ExecutionID identity.AgentExecutionID
	// PromptCacheKey 用于 Provider 的 Prompt Cache 路由，留空时不发送。
	PromptCacheKey  string
	Context         appcontext.ModelContext
	Model           Config
	MaxOutputTokens int
	Tools           []tool.ToolDefinition
	TurnID          identity.TurnID
	// OutputSchema 非空时启用严格 JSON Schema 输出；为空时不指定输出格式。
	OutputSchema *OutputSchema
}

// OutputSchema 定义结构化输出的名称与 JSON Schema，不携带 Provider 协议字段。
type OutputSchema struct {
	Name   string
	Schema json.RawMessage
}

// Validate 只读校验名称和 object 根结构；具体 Schema 关键字由 Provider 校验。
// 名称只能包含 ASCII 字母、数字、下划线和连字符，长度为 1 至 64 字节。
func (s OutputSchema) Validate() error {
	if len(s.Name) == 0 || len(s.Name) > 64 || strings.ContainsFunc(s.Name, func(character rune) bool {
		return !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-')
	}) {
		return errors.New("output schema name must contain 1 to 64 ASCII letters, digits, underscores or hyphens")
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(s.Schema, &schema); err != nil {
		return fmt.Errorf("decode output schema: %w", err)
	}
	var rootType string
	if err := json.Unmarshal(schema["type"], &rootType); err != nil || rootType != "object" {
		return errors.New("output schema must have an object root")
	}
	return nil
}
