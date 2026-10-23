package errorcode

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
