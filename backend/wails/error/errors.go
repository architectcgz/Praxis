package apperr

import (
	"context"
	"encoding/json"
	"errors"
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

// PublicError 保留业务码和安全的校验详情；未知内部错误不向前端暴露原始信息。
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
		message := ""
		var validation interface{ ValidationMessage() string }
		if errors.As(err, &validation) {
			message = validation.ValidationMessage()
		}
		return codedError{code: coded.ErrorCode(), message: message}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return codedError{code: ErrRequestCanceled.ErrorCode()}
	case errors.Is(err, context.DeadlineExceeded):
		return codedError{code: ErrRequestTimeout.ErrorCode()}
	}
	return codedError{code: ErrInternal.ErrorCode()}
}

type codedError struct {
	code    string
	message string
}

func (e codedError) Error() string { return Encode(e.code, e.message) }
