package apperr

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/errorcode"
	"strings"
)

// Coded 由携带稳定错误码的错误实现；Wails 边界据此透传，不依赖具体业务错误类型。
type Coded interface {
	ErrorCode() string
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

// PublicError 保留业务错误码，内部错误详情只写日志。
func PublicError(err error) error {
	if err == nil {
		return nil
	}
	var encoded codedError
	if errors.As(err, &encoded) {
		return encoded
	}
	var coded Coded
	if errors.As(err, &coded) {
		return codedError{code: coded.ErrorCode()}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return codedError{code: ErrRequestCanceled.ErrorCode()}
	case errors.Is(err, context.DeadlineExceeded):
		return codedError{code: ErrRequestTimeout.ErrorCode()}
	}
	return codedError{code: ErrInternal.ErrorCode()}
}

// PublicInputError 仅供输入校验出口调用，显式公开该请求的校验详情。
func PublicInputError(err error) error {
	if err == nil {
		return nil
	}
	var coded Coded
	if errors.As(err, &coded) && (errors.Is(err, errorcode.ErrInvalidValue) || errors.Is(err, errorcode.ErrInvalidRequest) || errors.Is(err, ErrInvalidRequest)) {
		return codedError{code: coded.ErrorCode(), message: InputMessage(err)}
	}
	return PublicError(err)
}

// InputMessage 从已确认的输入校验错误中取出展示文本，稳定码通过 wire 的 code 字段传递。
func InputMessage(err error) string {
	if err == nil {
		return ""
	}
	var coded Coded
	if !errors.As(err, &coded) || err.Error() == coded.ErrorCode() {
		return ""
	}
	return strings.TrimSuffix(err.Error(), ": "+coded.ErrorCode())
}

type codedError struct {
	code    string
	message string
}

func (e codedError) Error() string { return Encode(e.code, e.message) }
