// Package apperr 只定义 wails 层自身的错误码：入参校验与传输/宿主错误。
//
// 业务错误由 internal/contracts 定义，wails 层在出口原样透传其业务码，
package apperr

// ErrorCode 是 app 层自身错误的稳定码。
type ErrorCode string

const (
	// 入参校验
	ErrorCodeInvalidRequest ErrorCode = "invalid_request"
	ErrorCodeValidation     ErrorCode = "validation_error"

	// 传输/宿主
	ErrorCodeBindingUnavailable ErrorCode = "binding_unavailable"
	ErrorCodeConfiguration      ErrorCode = "configuration_error"
	ErrorCodeRequestCanceled    ErrorCode = "request_canceled"
	ErrorCodeRequestTimeout     ErrorCode = "request_timeout"

	// 兜底
	ErrorCodeInternal ErrorCode = "internal_error"
)

func (c ErrorCode) String() string { return string(c) }
