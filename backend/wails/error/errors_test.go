package apperr_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"praxis/internal/contracts"
	"praxis/internal/request"
	apperr "praxis/wails/error"
)

func TestRequestValidationErrorEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		message string
		invalid func() error
	}{
		{
			name:    "input",
			field:   "content",
			message: "content must be non-empty UTF-8 text within the size limit",
			invalid: func() error {
				_, err := request.NewSendInput(request.SendInput{ProviderID: "provider", ModelID: "model"})
				return err
			},
		},
		{
			name:    "queue",
			field:   "prompt",
			message: "prompt must be non-empty UTF-8 text within the size limit",
			invalid: func() error {
				_, err := request.NewEnqueueTask(request.EnqueueTask{ProviderID: "provider", ModelID: "model"})
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("validate request: %w", tc.invalid())
			if !errors.Is(err, contracts.ErrInvalidValue) {
				t.Fatalf("validation error lost its business code: %v", err)
			}
			var detail *contracts.ValidationError
			if !errors.As(err, &detail) || detail.Field != tc.field || detail.Message != tc.message {
				t.Fatalf("unexpected validation detail: %+v, %v", detail, err)
			}
			var envelope apperr.Envelope
			if decodeErr := json.Unmarshal([]byte(apperr.PublicError(err).Error()), &envelope); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if envelope.Code != "generic.invalid_value" || envelope.Message != "validate request: "+tc.field+": "+tc.message {
				t.Fatalf("unexpected error envelope: %+v", envelope)
			}
		})
	}
}

func TestBusinessErrorPreservesExternalCause(t *testing.T) {
	// 模拟外部系统返回的原始错误；语言规范不能翻译或替换此类内容。
	cause := errors.New("外部系统暂不可用")
	business := contracts.New(contracts.ProjectWorkspaceInvalid, "workspace is unavailable")
	business.Cause = cause
	err := fmt.Errorf("preview file: %w", business)
	var classified *contracts.Error
	if !errors.Is(err, cause) || !errors.As(err, &classified) || classified != business {
		t.Fatalf("external cause or business error identity was lost: %v", err)
	}
	var envelope apperr.Envelope
	if decodeErr := json.Unmarshal([]byte(apperr.PublicError(err).Error()), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if envelope.Code != "project.workspace_invalid" || envelope.Message != "preview file: workspace is unavailable" {
		t.Fatalf("unexpected business error envelope: %+v", envelope)
	}
	if errors.Unwrap(business).Error() != "外部系统暂不可用" {
		t.Fatalf("external error text changed: %v", business.Cause)
	}
}
