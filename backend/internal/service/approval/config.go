package approval

import (
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
)

// ConfigSnapshot 固定同一次配置发布的全局审批模式和工具规则。
// 值字段只属于当前副本，工具规则不暴露可变配置，取得后不再访问共享配置。
type ConfigSnapshot struct {
	GlobalMode security.ApprovalMode
	Tools      tool.CatalogSnapshot
}

// ConfigSource 由配置持有者负责同步读取和整体发布，业务侧不管理配置锁。
type ConfigSource interface {
	ApprovalConfig() ConfigSnapshot
}
