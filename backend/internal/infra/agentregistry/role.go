package agentregistry

import (
	"errors"
	"fmt"
	"io"
	"os"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/errorcode"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// roleMetadata 仅描述 ROLE.md 文件头，文件格式不进入领域模型。
type roleMetadata struct {
	Name string               `yaml:"name"`
	Role agentmodel.AgentRole `yaml:"role"`
}

func readRole(path string) (agentmodel.AgentDefinition, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return agentmodel.AgentDefinition{}, fmt.Errorf("read role %q: %w", path, err)
	}
	definition, err := parseRole(string(encoded))
	if err != nil {
		return agentmodel.AgentDefinition{}, fmt.Errorf("role %q: %w", path, err)
	}
	return definition, nil
}

func parseRole(content string) (agentmodel.AgentDefinition, error) {
	if !utf8.ValidString(content) {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md must be UTF-8: %w", errorcode.ErrInvalidValue)
	}
	// 换行及正文外围空行在文件读取边界处理，模型只接收 Markdown 正文。
	content = strings.ReplaceAll(content, "\r\n", "\n")
	remaining, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md requires YAML front matter: %w", errorcode.ErrInvalidValue)
	}
	header, body, ok := strings.Cut(remaining, "\n---\n")
	if !ok {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md front matter is not closed: %w", errorcode.ErrInvalidValue)
	}
	decoder := yaml.NewDecoder(strings.NewReader(header))
	decoder.KnownFields(true)
	var metadata roleMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md metadata: %w: %w", errorcode.ErrInvalidValue, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md metadata must contain one YAML document: %w", errorcode.ErrInvalidValue)
	}
	if metadata.Name == "" || metadata.Name != strings.TrimSpace(metadata.Name) || strings.ContainsFunc(metadata.Name, unicode.IsControl) {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md name must be canonical, nonempty and contain no control characters: %w", errorcode.ErrInvalidValue)
	}
	if !metadata.Role.Valid() {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md role: unknown agent role: %w", errorcode.ErrInvalidValue)
	}
	instructions := strings.Trim(body, "\n")
	if instructions == "" || instructions != strings.TrimSpace(instructions) {
		return agentmodel.AgentDefinition{}, fmt.Errorf("ROLE.md body must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
	}
	return agentmodel.AgentDefinition{
		Name:         metadata.Name,
		Role:         metadata.Role,
		Instructions: instructions,
	}, nil
}
