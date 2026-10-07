package model

// Model 根据冻结模型配置提供模型流和输出预算，凭据只在运行时读取。
// 上下文压缩使用请求中的冻结 ModelSnapshot；最终编码请求的窗口校验归 Provider adapter。
type Model struct {
	Stream          ModelStream
	MaxOutputTokens int
}

// ModelBuilder 按已冻结的模型快照构建本次执行的模型。
type ModelBuilder interface {
	// BuildModel 返回模型流和输出预算；快照无效或 Provider 无法构建时返回错误。
	BuildModel(ModelSnapshot) (Model, error)
}
