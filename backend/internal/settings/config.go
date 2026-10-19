package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"praxis/internal/core/security"
)

// Config 保存全局授权配置，项目可以覆盖 ApprovalMode。
type Config struct {
	ApprovalMode security.ApprovalMode `json:"approvalMode"`
}

// Load 只读加载现有配置；缺失、非法字段和非 canonical 模式均返回错误，不创建或修改文件。
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read settings: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value Config
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("decode settings: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("settings contain trailing JSON data")
	}
	if !value.ApprovalMode.ValidDefault() {
		return Config{}, errors.New("settings approval mode must be ask, auto or yolo")
	}
	return value, nil
}
