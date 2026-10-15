package context

import "errors"

// Limits 是模型窗口及输出预留，不包含业务资源预算。
type Limits struct {
	ContextWindow   int
	MaxOutputTokens int
}

func (l Limits) Validate() error {
	if l.ContextWindow <= 0 || l.MaxOutputTokens < 0 || l.MaxOutputTokens >= l.ContextWindow {
		return errors.New("context window and output reserve are invalid")
	}
	return nil
}
