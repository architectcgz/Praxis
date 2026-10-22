package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"strings"
)

// Request 是发送给 Provider 的一次完整模型上下文请求，不包含凭据字段。
type Request struct {
	ExecutionID identity.AgentExecutionID
	// PromptCacheKey 用于 Provider 的 Prompt Cache 路由，留空时不发送。
	PromptCacheKey  string
	Context         appcontext.ModelContext
	Model           model.Config
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

// Clone 深拷贝请求内容，调用方、扩展和 Provider 不共享可变上下文、Schema 或工具定义。
func (r Request) Clone() Request {
	r.Context = r.Context.Clone()
	r.Model = r.Model.Clone()
	if r.Tools != nil {
		tools := make([]tool.ToolDefinition, len(r.Tools))
		for index, definition := range r.Tools {
			definition.InputSchema = bytes.Clone(definition.InputSchema)
			tools[index] = definition
		}
		r.Tools = tools
	}
	if r.OutputSchema != nil {
		schema := *r.OutputSchema
		schema.Schema = bytes.Clone(schema.Schema)
		r.OutputSchema = &schema
	}
	return r
}
