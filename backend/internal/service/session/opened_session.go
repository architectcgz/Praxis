package session

import (
	sessionmodel "praxis/internal/core/session"
	"sync"
)

// openedSession 登记核心会话及本次加载的配置；配置不参与会话日志提交。
// configMu 保护只读配置的整体替换和模型选择。
type openedSession struct {
	session        *sessionmodel.Session
	configMu       sync.RWMutex
	config         *Configuration
	modelSelection sessionmodel.ModelSelection
}
