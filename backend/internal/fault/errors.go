// Package fault 提供通用错误载体、输入错误和状态转换错误，不集中业务错误码。
package fault

import (
	"fmt"
	"strings"
)

// Code 是业务错误码。
type Code string

// Error 是业务层统一的错误载体。
type Error struct {
	Code    Code
	Message string
	Cause   error
}

// New 构造一个业务错误。
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Error 实现 error；有 Message 时优先返回 Message，否则退化为业务码。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// Unwrap 暴露 cause，供内部 errors.Is/As 判断。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ErrorCode 返回稳定业务码，供边界（app 层）序列化透传。
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}

// Is 允许按业务码匹配，使包装后的错误仍可用 errors.Is 判断。
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other != nil && e != nil && other.Code == e.Code
}

// 通用/校验域：与具体业务对象无关的请求与基础设施类错误。
const (
	InvalidValueCode      Code = "generic.invalid_value"
	InvalidTransitionCode Code = "generic.invalid_transition"
	InvalidRequest        Code = "generic.invalid_request"
	NotFound              Code = "generic.not_found"
	RequestCanceled       Code = "generic.request_canceled"
	RequestTimeout        Code = "generic.request_timeout"
	Internal              Code = "generic.internal_error"
)

// 请求冲突不依赖具体聚合，由持久化和边界统一识别。
const RequestConflict Code = "request.conflict"

// 领域哨兵错误统一承载业务码，app 层据业务码翻译为前端错误码。
var (
	ErrInvalidValue      = New(InvalidValueCode, "")
	ErrInvalidTransition = New(InvalidTransitionCode, "")
	ErrRequestConflict   = New(RequestConflict, "")
	ErrNotFound          = New(NotFound, "")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error { return ErrInvalidValue }

// ValidationField 供输入和传输边界通过窄接口识别可反馈给用户的字段错误。
func (e *ValidationError) ValidationField() string { return e.Field }

// InvalidValue 为领域字段构造校验错误。
func InvalidValue(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// FieldError 为已有校验错误添加外层字段前缀，使嵌套校验报告完整路径。
func FieldError(field string, err error) error {
	if err == nil {
		return nil
	}
	return InvalidValue(field, strings.TrimPrefix(err.Error(), field+": "))
}

type TransitionError struct {
	Entity string
	From   string
	To     string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s cannot transition from %q to %q", e.Entity, e.From, e.To)
}

func (e *TransitionError) Unwrap() error { return ErrInvalidTransition }

// InvalidTransition 为领域状态机构造状态转换错误。
func InvalidTransition(entity, from, to string) error {
	return &TransitionError{Entity: entity, From: from, To: to}
}
