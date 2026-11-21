// Package errorcode 定义可直接返回和匹配的稳定业务错误码。
package errorcode

import "errors"

// Code 同时实现 error 和传输边界识别的错误码能力。
type Code string

func (c Code) Error() string { return string(c) }

func (c Code) ErrorCode() string { return string(c) }

type wrappedError struct {
	code  Code
	cause error
}

func (e wrappedError) Error() string {
	if e.cause == nil {
		return string(e.code)
	}
	return e.cause.Error()
}

func (e wrappedError) ErrorCode() string { return string(e.code) }

func (e wrappedError) Unwrap() []error { return []error{e.cause, e.code} }

// Wrap 为底层错误附加稳定错误码，不污染展示文本，支持 errors.Is 同时匹配原错误和错误码。
func (c Code) Wrap(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, c) {
		return err
	}
	return wrappedError{code: c, cause: err}
}

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
	ErrStorage           Code = "execution.storage_error"
	ErrProvider          Code = "execution.provider_error"
	ErrContract          Code = "execution.contract_error"
)
