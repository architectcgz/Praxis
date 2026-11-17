// Package errorcode 定义可直接返回和匹配的稳定业务错误码。
package errorcode

// Code 同时实现 error 和传输边界识别的错误码能力。
type Code string

func (c Code) Error() string { return string(c) }

func (c Code) ErrorCode() string { return string(c) }

// 通用错误不依赖具体业务对象；业务错误由所属包定义。
const (
	ErrInvalidValue      Code = "generic.invalid_value"
	ErrInvalidTransition Code = "generic.invalid_transition"
	ErrInvalidRequest    Code = "generic.invalid_request"
	ErrNotFound          Code = "generic.not_found"
	ErrRequestCanceled   Code = "generic.request_canceled"
	ErrRequestTimeout    Code = "generic.request_timeout"
	ErrInternal          Code = "generic.internal_error"
	ErrRequestConflict   Code = "request.conflict"
)
