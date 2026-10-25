package tool

import "praxis/internal/core/security"

// CatalogSnapshot 固定工具可用性和命令策略，只提供读取能力。
// 注册表在启动后不再修改；可重载的 Map 和规则切片由快照独立持有。
type CatalogSnapshot struct {
	config   Config
	registry ToolCatalog
}

// Snapshot 复制当前配置，后续目录重载不会改变已取得的快照。
func (c *Catalog) Snapshot() CatalogSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CatalogSnapshot{
		config:   c.config.clone(),
		registry: c.registry,
	}
}

func (s CatalogSnapshot) Get(name ToolName) (Tool, bool) {
	if configured, specified := s.config.Tools[name]; specified && !configured.Enabled {
		return nil, false
	}
	return s.registry.Get(name)
}

func (s CatalogSnapshot) DecideCommand(command string) security.ApprovalDecision {
	return s.config.Bash.Decide(command)
}
