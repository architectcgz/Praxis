// Package apperr 定义 Wails 层自身的错误码和传输错误映射。
package apperr

import "praxis/internal/errorcode"

const (
	ErrInvalidRequest     errorcode.Code = "invalid_request"
	ErrValidation         errorcode.Code = "validation_error"
	ErrBindingUnavailable errorcode.Code = "binding_unavailable"
	ErrConfiguration      errorcode.Code = "configuration_error"
	ErrRequestCanceled    errorcode.Code = "request_canceled"
	ErrRequestTimeout     errorcode.Code = "request_timeout"
	ErrInternal           errorcode.Code = "internal_error"
)
