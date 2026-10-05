package apperr

import (
	"context"
	"encoding/json"
	"errors"
)

// Coded 由携带稳定错误码的错误实现；app 层据此透传，不依赖任何具体业务错误类型。
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

// PublicError 保留业务码和错误详情，供桌面端显示具体失败原因。
// 仅有业务码或属于取消、超时的错误由前端提供文案；未知错误使用 internal_error。
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
		message := err.Error()
		if message == coded.ErrorCode() {
			message = ""
		}
		return codedError{code: coded.ErrorCode(), message: message}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return codedError{code: ErrorCodeRequestCanceled.String()}
	case errors.Is(err, context.DeadlineExceeded):
		return codedError{code: ErrorCodeRequestTimeout.String()}
	}
	return codedError{code: ErrorCodeInternal.String(), message: err.Error()}
}

// CodedError 构造 app 层自身的错误码错误，无附加信息。
func CodedError(code ErrorCode) error {
	return codedError{code: code.String()}
}

type codedError struct {
	code    string
	message string
}

func (e codedError) Error() string { return Encode(e.code, e.message) }
