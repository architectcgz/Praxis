package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"praxis/internal/agent"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const agentDefinitionFile = "AGENT.md"

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// LoadAgentDefinitions 按定义 ID 返回角色定义；一级目录名就是定义 ID。
func LoadAgentDefinitions(root string) ([]agent.AgentDefinition, error) {
	definitions, err := readAgentDefinitions(root)
	if err != nil {
		if _, ok := errors.AsType[*ConfigurationError](err); ok {
			return nil, err
		}
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
		definition, err := readAgentDefinition(filepath.Join(directory, agentDefinitionFile))
		if err != nil {
			return nil, err
		}
		definition.ID = id
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

// agentDefinitionMetadata 仅描述 AGENT.md 文件头，文件格式不进入领域模型。
type agentDefinitionMetadata struct {
	Name string          `yaml:"name"`
	Role agent.AgentRole `yaml:"role"`
}

func readAgentDefinition(path string) (agent.AgentDefinition, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return agent.AgentDefinition{}, &ConfigurationError{Path: path, Err: err}
	}
	return parseAgentDefinition(path, string(encoded))
}

func parseAgentDefinition(path, content string) (agent.AgentDefinition, error) {
	if !utf8.ValidString(content) {
		return invalidAgentDefinition(path, "AGENT.md must be UTF-8.", nil)
	}
	// 换行及正文外围空行在文件读取边界处理，模型只接收 Markdown 正文。
	content = strings.ReplaceAll(content, "\r\n", "\n")
	remaining, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return invalidAgentDefinition(path, "AGENT.md requires YAML front matter delimited by --- with name and role fields.", nil)
	}
	header, body, ok := strings.Cut(remaining, "\n---\n")
	if !ok {
		return invalidAgentDefinition(path, "AGENT.md YAML front matter must end with a separate --- line.", nil)
	}
	decoder := yaml.NewDecoder(strings.NewReader(header))
	decoder.KnownFields(true)
	var metadata agentDefinitionMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return invalidAgentDefinition(path, "AGENT.md metadata must be valid YAML containing only string name and role fields.", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return invalidAgentDefinition(path, "AGENT.md metadata must contain exactly one YAML document.", err)
	}
	if metadata.Name == "" || metadata.Name != strings.TrimSpace(metadata.Name) || strings.ContainsFunc(metadata.Name, unicode.IsControl) {
		return invalidAgentDefinition(path, "AGENT.md name must be nonempty, have no surrounding whitespace and contain no control characters.", nil)
	}
	if !metadata.Role.Valid() {
		return invalidAgentDefinition(path, "AGENT.md role must be primary, delegate, advisor or curator.", nil)
	}
	instructions := strings.Trim(body, "\n")
	if instructions == "" || instructions != strings.TrimSpace(instructions) {
		return invalidAgentDefinition(path, "AGENT.md body must be nonempty and have no surrounding whitespace other than blank lines.", nil)
	}
	return agent.AgentDefinition{
		Name:         metadata.Name,
		Role:         metadata.Role,
		Instructions: instructions,
	}, nil
}

func invalidAgentDefinition(path, detail string, cause error) (agent.AgentDefinition, error) {
	err := fmt.Errorf("%s: %w", detail, errorcode.ErrInvalidValue)
	if cause != nil {
		err = fmt.Errorf("%w: %w", err, cause)
	}
	return agent.AgentDefinition{}, &ConfigurationError{
		Path:   path,
		Err:    err,
		Detail: detail,
	}
}
