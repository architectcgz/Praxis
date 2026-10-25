// Package validation 使用 DTO 标签和统一字段入口校验 Wails 输入。
// 输入规范化由 Binding 完成，资源状态、引用关系等业务约束由所属业务模块负责。
package validation

import (
	"errors"
	"fmt"
	modelconfig "praxis/internal/core/model/config"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	projectservice "praxis/internal/service/project"
	"praxis/internal/utils/pathutil"
	apperr "praxis/wails/error"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
	"github.com/go-playground/validator/v10/non-standard/validators"
)

var (
	configIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	// 校验器仅在初始化时注册规则，之后并发复用规则和结构体元数据缓存。
	requestValidator = newValidator()
)

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	for name, rule := range map[string]validator.Func{
		"notblank": validators.NotBlank,
		"normalized": func(field validator.FieldLevel) bool {
			value := field.Field().String()
			return value == strings.TrimSpace(value)
		},
		"no_control": func(field validator.FieldLevel) bool {
			return !strings.ContainsFunc(field.Field().String(), unicode.IsControl)
		},
		"project_identity": func(field validator.FieldLevel) bool {
			return identity.ProjectID(field.Field().String()).Valid()
		},
		"session_identity": func(field validator.FieldLevel) bool {
			return identity.SessionID(field.Field().String()).Valid()
		},
		"config_id_format": func(field validator.FieldLevel) bool {
			return configIDPattern.MatchString(field.Field().String())
		},
		"definition_id_format": func(field validator.FieldLevel) bool {
			return definitionIDPattern.MatchString(field.Field().String())
		},
		"project_name_format": func(field validator.FieldLevel) bool {
			value := field.Field().String()
			return value != "." && value != ".." && !strings.ContainsAny(value, "\\/:*?\"<>|\x00\r\n")
		},
		"absolute_path": func(field validator.FieldLevel) bool {
			return pathutil.IsAbsoluteNormalized(field.Field().String())
		},
		"preview_path": func(field validator.FieldLevel) bool {
			value := field.Field().String()
			return !strings.ContainsRune(value, '\x00') && !strings.HasPrefix(value, `\\`) && !strings.HasPrefix(value, "//")
		},
		"task_text": func(field validator.FieldLevel) bool {
			value := field.Field().String()
			// validator 的 max 按字符计数，正文协议限制必须按 UTF-8 字节计算。
			return utf8.ValidString(value) && len(value) <= sessionmodel.MaxInputBytes
		},
		"model_base_url": func(field validator.FieldLevel) bool {
			_, err := modelconfig.ValidateModelBaseURL(field.Field().String())
			return err == nil
		},
		"model_proxy_url": func(field validator.FieldLevel) bool {
			_, err := modelconfig.ValidateModelProxyURL(field.Field().String())
			return err == nil
		},
	} {
		if err := v.RegisterValidation(name, rule); err != nil {
			panic(fmt.Errorf("register %s validation: %w", name, err))
		}
	}
	// 身份不自动修剪；项目、会话规则由业务身份类型统一定义，输入标签只负责接入。
	v.RegisterAlias("id", "required,notblank,normalized,no_control")
	v.RegisterAlias("project_id", "required,project_identity")
	v.RegisterAlias("session_id", "required,session_identity")
	v.RegisterAlias("config_id", "required,normalized,config_id_format")
	v.RegisterAlias("definition_id", "required,normalized,definition_id_format")
	v.RegisterAlias("project_name", "required,notblank,project_name_format")
	v.RegisterAlias("project_path", "required,absolute_path")
	v.RegisterAlias("task_prompt", "required,notblank,task_text")
	return v
}

// Struct 按 DTO 的 validate 标签校验请求，嵌套字段保留 JSON 路径和数组下标。
func Struct(request any) error {
	return inputError(requestValidator.Struct(request), "")
}

// Value 按标签校验独立参数，不改变现有 Wails 方法签名。
func Value(field string, value any, tags string) error {
	return inputError(requestValidator.Var(value, tags), field)
}

// ID 校验独立的身份参数，规则与 DTO 的 id 标签一致。
func ID(field, value string) error {
	return Value(field, value, "id")
}

func inputError(err error, field string) error {
	if err == nil {
		return nil
	}
	failures, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok {
		// 无效的校验器调用属于内部错误，不作为用户输入问题公开。
		return fmt.Errorf("request validation failed: %w", err)
	}
	// 只返回首个失败字段，且不回显输入值，避免泄露请求中的敏感内容。
	failure := failures[0]
	if field == "" {
		field = failure.Field()
		if _, path, found := strings.Cut(failure.Namespace(), "."); found {
			field = path
		}
	}
	// 保留身份、普通字段和项目工作区输入各自使用的公开错误码。
	var code error = errorcode.ErrInvalidValue
	switch failure.Tag() {
	case "id", "project_id", "session_id", "definition_id", "required_with":
		code = apperr.ErrInvalidRequest
	case "project_name", "project_path":
		code = projectservice.ErrWorkspaceInvalid
	}
	reason := "must satisfy " + failure.ActualTag()
	switch failure.ActualTag() {
	case "required", "notblank", "required_with":
		reason = "required"
	case "normalized":
		reason = "must be normalized"
	case "no_control":
		reason = "must not contain control characters"
	case "project_identity", "session_identity":
		reason = "must be a valid resource identity of at most 180 characters"
	case "config_id_format", "definition_id_format":
		reason = "invalid identity format"
	case "project_name_format":
		reason = "invalid project name"
	case "absolute_path":
		reason = "must be an absolute normalized path"
	case "preview_path":
		reason = "file path is invalid"
	case "oneof":
		reason = "must be one of " + failure.Param()
	case "model_base_url", "model_proxy_url":
		reason = "invalid model URL"
	}
	if failure.Tag() == "task_prompt" {
		reason = "prompt must be non-empty UTF-8 text within the size limit"
	}
	return fmt.Errorf("%s: %s: %w", field, reason, code)
}
