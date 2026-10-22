package hook

import "fmt"

// Error 标记扩展回调失败，业务层可与 Provider、工具及存储错误区分。
type Error struct {
	Point string
	Cause error
}

func (e *Error) Error() string { return fmt.Sprintf("%s hook: %v", e.Point, e.Cause) }
func (e *Error) Unwrap() error { return e.Cause }
