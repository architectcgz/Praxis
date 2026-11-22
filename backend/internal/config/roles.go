package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/agent"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"regexp"
	"strings"
)

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// LoadAgentDefinitions 按定义 ID 返回角色定义；一级目录名就是定义 ID。
func LoadAgentDefinitions(root string) ([]agent.AgentDefinition, error) {
	definitions, err := readAgentDefinitions(root)
	if err != nil {
		return nil, &ConfigurationError{
			Path: root,
			Err:  err,
		}
	}
	return definitions, nil
}

func readAgentDefinitions(root string) ([]agent.AgentDefinition, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("agent definitions directory is required")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read agent definitions directory: %w", err)
	}
	// os.ReadDir 已按目录项名称排序，定义 ID 沿用目录名，无需重排。
	var definitions []agent.AgentDefinition
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("agent definition %q cannot be a symbolic link", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		id := identity.AgentDefinitionID(entry.Name())
		if !definitionIDPattern.MatchString(id.String()) {
			return nil, fmt.Errorf("agentDefinition.id: definition id is invalid: %w", errorcode.ErrInvalidValue)
		}
		directory := filepath.Join(root, entry.Name())
		definition, err := readRole(filepath.Join(directory, "ROLE.md"))
		if err != nil {
			return nil, err
		}
		definition.ID = id
		definitions = append(definitions, definition)
	}
	return definitions, nil
}
