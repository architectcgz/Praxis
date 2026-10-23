// Package errorcode 定义可直接返回和匹配的稳定业务错误码。
package errorcode

// Code 同时实现 error 和传输边界识别的错误码能力。
type Code string

func (c Code) Error() string { return string(c) }

func (c Code) ErrorCode() string { return string(c) }
