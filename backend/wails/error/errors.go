// Package apperr 定义 Wails 层自身的错误码和传输错误映射。
package apperr

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/errorcode"
	"strings"
)

const (
	ErrInvalidRequest     errorcode.Code = "invalid_request"
	ErrBindingUnavailable errorcode.Code = "binding_unavailable"
	ErrRequestCanceled    errorcode.Code = "request_canceled"
	ErrRequestTimeout     errorcode.Code = "request_timeout"
	ErrStartupFailed      errorcode.Code = "application.startup_failed"
)

// Coded 由携带稳定错误码的错误实现；Wails 边界据此透传，不依赖具体业务错误类型。
type Coded interface {
	error
	ErrorCode() string
}

// publicMessage 由错误所属模块实现，仅提供明确允许展示的说明。
type publicMessage interface {
	PublicMessage() string
}

// Envelope 是绑定错误的 wire 结构：稳定码 + 可读信息。
// message 为空时由前端按 code 取文案。
type Envelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Encode 把码与信息编码为 wire 信封。
func Encode(code, message string) string {
	encoded, err := json.Marshal(Envelope{Code: code, Message: message})
	if err != nil {
		return code
	}
	return string(encoded)
}

// PublicError 保留最外层业务分类和该错误明确公开的说明，原始诊断详情只写日志。
func PublicError(err error) error {
	return publicError(err, errorcode.ErrInternal)
}

// PublicStartupError 保留已有分类，仅将无法分类的启动错误归为启动失败。
func PublicStartupError(err error) error {
	return publicError(err, ErrStartupFailed)
}

func publicError(err error, fallback errorcode.Code) error {
	if err == nil {
		return nil
	}
	if coded, ok := errors.AsType[Coded](err); ok {
		result := codedError{code: coded.ErrorCode()}
		// 说明必须来自拥有主错误码的同一个错误，不能取底层其他语义的说明。
		if public, ok := coded.(publicMessage); ok {
			result.message = public.PublicMessage()
		}
		return result
	}
	switch {
	case errors.Is(err, context.Canceled):
		return codedError{code: ErrRequestCanceled.ErrorCode()}
	case errors.Is(err, context.DeadlineExceeded):
		return codedError{code: ErrRequestTimeout.ErrorCode()}
	}
	return codedError{code: fallback.ErrorCode()}
}

// PublicInputError 仅供输入校验出口调用，显式公开该请求的校验详情。
func PublicInputError(err error) error {
	if err == nil {
		return nil
	}
	if coded, ok := errors.AsType[Coded](err); ok {
		if _, public := coded.(publicMessage); public {
			return PublicError(err)
		}
		if coded.ErrorCode() == errorcode.ErrInvalidValue.ErrorCode() || coded.ErrorCode() == ErrInvalidRequest.ErrorCode() {
			return codedError{code: coded.ErrorCode(), message: InputMessage(err)}
		}
	}
	return PublicError(err)
}

// InputMessage 从已确认的输入校验错误中取出展示文本，稳定码通过 wire 的 code 字段传递。
func InputMessage(err error) string {
	if err == nil {
		return ""
	}
	coded, ok := errors.AsType[Coded](err)
	if !ok || err.Error() == coded.ErrorCode() {
		return ""
	}
	return strings.TrimSuffix(err.Error(), ": "+coded.ErrorCode())
}

type codedError struct {
	code    string
	message string
}

func (e codedError) Error() string { return Encode(e.code, e.message) }

func (e codedError) ErrorCode() string { return e.code }

func (e codedError) PublicMessage() string { return e.message }
