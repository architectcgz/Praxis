package toolconfig

import (
	"os"
	"path/filepath"
	"testing"

	"praxis/internal/contracts"
	"praxis/internal/core/security"
	"praxis/internal/tools"
)

func TestDefaultsAndReloadValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.json")
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewToolRegistry()
	catalog := NewCatalog(config, registry)
	if len(catalog.List()) != len(registry.List()) || catalog.DecideCommand("git status").Action != security.CommandAsk {
		t.Fatal("默认配置错误")
	}
	encoded := `{"tools":{"bash":{"enabled":false}},"bash":{"defaultAction":"deny","rules":[{"id":" status ","action":"allow","match":{"type":"exact","value":"git status"}}]}}`
	if err := os.WriteFile(path, []byte(encoded), 0600); err != nil {
		t.Fatal(err)
	}
	candidate, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.ValidateRegistered([]contracts.ToolName{contracts.ToolBash, contracts.ToolReadFile, contracts.ToolApplyPatch}); err != nil {
		t.Fatal(err)
	}
	catalog.ReplaceFrom(NewCatalog(candidate, registry))
	if _, enabled := catalog.Get(contracts.ToolBash); enabled {
		t.Fatal("重载未禁用 Bash")
	}
	if got := catalog.DecideCommand("git status"); got.RuleID != "status" || got.Action != security.CommandAllow {
		t.Fatal(got)
	}
	candidate.Tools[contracts.ToolReadFile] = ToolConfig{Enabled: false}
	candidate.Bash.Rules[0].Action = security.CommandDeny
	if _, enabled := catalog.Get(contracts.ToolReadFile); !enabled {
		t.Fatal("配置副本与运行中目录共享 map")
	}
	if catalog.DecideCommand("git status").Action != security.CommandAllow {
		t.Fatal("运行中规则被外部修改")
	}
	for _, invalid := range []string{
		`{"bash":{"rules":[{"id":"bad","action":"allow","match":{"type":"prefix","value":"git "}}]}}`,
		`{"tools":{"bash":{"enabled":true,"requiresWrite":true}}}`,
		`{"bash":{"defaultAction":"invalid"}}`,
		`{} {}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("非法重载被接受：%s", invalid)
		}
		if catalog.DecideCommand("git status").Action != security.CommandAllow {
			t.Fatal("非法配置改变了已生效规则")
		}
	}
	if err := (Config{Tools: map[contracts.ToolName]ToolConfig{"unregistered": {Enabled: true}}}).ValidateRegistered([]contracts.ToolName{contracts.ToolBash}); err == nil {
		t.Fatal("未注册工具配置被接受")
	}
}
