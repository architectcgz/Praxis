package toolregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"strings"
)

// Load 只读加载工具配置，缺失或非法文件直接返回错误。
func Load(path string) (tool.Config, error) {
	var config tool.Config
	encoded, err := os.ReadFile(path)
	if err != nil {
		return tool.Config{}, fmt.Errorf("read tool config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return tool.Config{}, fmt.Errorf("decode tool config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return tool.Config{}, errors.New("tool config contains multiple JSON values")
		}
		return tool.Config{}, fmt.Errorf("decode trailing tool config: %w", err)
	}
	if err := validateCommandPolicy(config.Bash); err != nil {
		return tool.Config{}, err
	}
	return config, nil
}

// validateCommandPolicy 在配置文件输入边界检查规则，匹配值保留空格和换行。
func validateCommandPolicy(policy security.CommandPolicy) error {
	seen := make(map[string]bool, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.ID == "" || rule.ID != strings.TrimSpace(rule.ID) || seen[rule.ID] {
			return fmt.Errorf("bash rule id %q must be unique and canonical", rule.ID)
		}
		seen[rule.ID] = true
		if !rule.Action.Valid() || strings.TrimSpace(rule.Match.Value) == "" {
			return fmt.Errorf("bash rule %q needs a valid action and non-empty match", rule.ID)
		}
		if rule.Match.Type != "exact" && rule.Match.Type != "prefix" {
			return fmt.Errorf("bash rule %q has an invalid match type", rule.ID)
		}
	}
	return nil
}
