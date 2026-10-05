// Package agentruntime 管理 Agent 的进程内执行生命周期，以及模型、工具和消息接口。
// 具体执行循环与适配器由 compose 创建并注入，本包不依赖它们的实现。
package agentruntime
