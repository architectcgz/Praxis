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

// PublicError 只做出口序列化：
//   - 业务错误：原样透传其业务码，message 留空由前端按码取文案；
//   - 上下文错误：映射为传输码；
//   - 未知错误：收敛为 internal_error，避免泄漏内部信息。
func PublicError(err error) error {
	if err == nil {
		return nil
	}
	var coded Coded
	if errors.As(err, &coded) {
		return codedError{code: coded.ErrorCode()}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return codedError{code: ErrorCodeRequestCanceled.String()}
	case errors.Is(err, context.DeadlineExceeded):
		return codedError{code: ErrorCodeRequestTimeout.String()}
	}
	return codedError{code: ErrorCodeInternal.String()}
}

// CodedError 构造 app 层自身的错误码错误，无附加信息。
func CodedError(code ErrorCode) error {
	return codedError{code: code.String()}
}

// CodedErrorf 构造 app 层错误码错误，并携带具体信息（如字段校验细节）。
func CodedErrorf(code ErrorCode, message string) error {
	return codedError{code: code.String(), message: message}
}

type codedError struct {
	code    string
	message string
}

func (e codedError) Error() string { return Encode(e.code, e.message) }
