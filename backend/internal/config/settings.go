package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"praxis/internal/security"
)

// Settings 保存全局授权配置，会话可以覆盖 ApprovalMode。
type Settings struct {
	ApprovalMode security.ApprovalMode `json:"approvalMode"`
}

// LoadSettings 只读加载现有配置；缺失、非法字段和非 canonical 模式均返回错误，不创建或修改文件。
func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, &ConfigurationError{Path: path, Err: fmt.Errorf("read settings: %w", err)}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value Settings
	if err := decoder.Decode(&value); err != nil {
		return Settings{}, &ConfigurationError{
			Path:   path,
			Err:    fmt.Errorf("decode settings: %w", err),
			Detail: "Settings must be a JSON object containing only a valid approvalMode field.",
		}
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("settings contain trailing JSON data")
		}
		return Settings{}, &ConfigurationError{
			Path:   path,
			Err:    fmt.Errorf("decode trailing settings: %w", err),
			Detail: "Settings must contain exactly one JSON object without trailing data.",
		}
	}
	if !value.ApprovalMode.Valid() {
		return Settings{}, &ConfigurationError{
			Path:   path,
			Err:    errors.New("settings approval mode is invalid"),
			Detail: "approvalMode must be ask, auto or yolo.",
		}
	}
	return value, nil
}
