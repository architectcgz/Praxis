// Package fault 只定义携带字段和状态转换详情的结构化错误。
package fault

import (
	"fmt"
	"praxis/internal/errorcode"
	"strings"
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

func (e *ValidationError) Unwrap() error { return errorcode.ErrInvalidValue }

// ValidationField 供输入和传输边界通过窄接口识别可反馈给用户的字段错误。
func (e *ValidationError) ValidationField() string { return e.Field }

// ValidationMessage 只公开该字段的校验详情，不包含外层包装上下文。
func (e *ValidationError) ValidationMessage() string { return e.Error() }

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

func (e *TransitionError) Unwrap() error { return errorcode.ErrInvalidTransition }

// InvalidTransition 为领域状态机构造状态转换错误。
func InvalidTransition(entity, from, to string) error {
	return &TransitionError{Entity: entity, From: from, To: to}
}
