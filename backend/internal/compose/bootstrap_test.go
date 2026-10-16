package compose

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"praxis/internal/core/security"
	agentregistry "praxis/internal/infra/agent_registry"
	"praxis/internal/infra/dataroot"
	modelregistry "praxis/internal/infra/model_registry"
	"praxis/internal/infra/toolconfig"
)

func bootstrapRoot(t *testing.T) dataroot.DataRoot {
	t.Helper()
	root, err := dataroot.Resolve(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBootstrapConcurrentAndIdempotent(t *testing.T) {
	root := bootstrapRoot(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := initializeDefaultFiles(context.Background(), root); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if _, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := agentregistry.Load(root.AgentDefinitions, func(string, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	tools, err := toolconfig.Load(root.ToolConfig)
	if err != nil || tools.Bash.DefaultAction != security.CommandAsk {
		t.Fatal(tools, err)
	}
	custom := []byte("# Custom Agent\n\n保留用户指令")
	path := filepath.Join(root.AgentDefinitions, "primary", "AGENT.md")
	if err := os.WriteFile(path, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root.ModelCredentialsFile); err != nil {
		t.Fatal(err)
	}
	if err := initializeDefaultFiles(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(custom) {
		t.Fatal("bootstrap overwrote existing instructions", err)
	}
	if _, err := os.Stat(root.ModelCredentialsFile); err != nil {
		t.Fatal(err)
	}
}

func TestLoadIsReadOnlyAndRejectsMissingOrNonCanonicalFiles(t *testing.T) {
	root := bootstrapRoot(t)
	if _, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil); err == nil {
		t.Fatal("missing models were accepted")
	}
	if _, err := os.Stat(root.ModelProvidersConfig); !os.IsNotExist(err) {
		t.Fatal("Load created models", err)
	}
	if _, err := toolconfig.Load(root.ToolConfig); err == nil {
		t.Fatal("missing tools were accepted")
	}
	if _, err := agentregistry.Load(filepath.Join(root.AgentDefinitions, "absent"), func(string, string) error { return nil }); err == nil {
		t.Fatal("missing agents were accepted")
	}
	if err := initializeDefaultFiles(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root.ModelCredentialsFile); err != nil {
		t.Fatal(err)
	}
	if _, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil); err == nil {
		t.Fatal("missing credentials were accepted")
	}
	if _, err := os.Stat(root.ModelCredentialsFile); !os.IsNotExist(err) {
		t.Fatal("Load created auth", err)
	}
	path := filepath.Join(root.AgentDefinitions, "primary", "AGENT.md")
	raw := []byte(" custom prompt \n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentregistry.Load(root.AgentDefinitions, func(string, string) error { return nil }); err == nil {
		t.Fatal("non-canonical instructions were accepted")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != string(raw) {
		t.Fatal("Load modified instructions")
	}
}

func TestFreshApplicationAndReloadNeverRecreatesConfig(t *testing.T) {
	root := bootstrapRoot(t)
	app, err := Open(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := os.Remove(root.ToolConfig); err != nil {
		t.Fatal(err)
	}
	if err := app.frontend.ReloadConfig(context.Background()); err == nil {
		t.Fatal("reload accepted a missing file")
	}
	if _, err := os.Stat(root.ToolConfig); !os.IsNotExist(err) {
		t.Fatal("reload recreated tools", err)
	}
}
