package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverProviderModelsRequestsConfiguredCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("request method = %s, want %s", request.Method, http.MethodGet)
		}
		if request.URL.Path != "/v1/models" {
			t.Errorf("request path = %s, want /v1/models", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer test-key" {
			t.Errorf("authorization = %q, want bearer key", authorization)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"id":"gpt-5-mini"},{"id":"gpt-5"},{"id":"gpt-5-mini"},{"id":""}]}`))
	}))
	defer server.Close()

	registry := &Registry{
		client:  server.Client(),
		secrets: map[string]string{"gateway": "test-key"},
		byProvider: map[string]ProviderConfig{
			"gateway": {
				ID: "gateway", BaseURL: server.URL,
			},
		},
	}

	models, err := registry.DiscoverProviderModels(context.Background(), "gateway")
	if err != nil {
		t.Fatalf("discover provider models: %v", err)
	}
	want := []string{"gpt-5", "gpt-5-mini"}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
}

func TestDiscoverProviderModelsRejectsInvalidCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"models":[{"id":"gpt-5"}]}`))
	}))
	defer server.Close()

	registry := &Registry{
		client:  server.Client(),
		secrets: map[string]string{"gateway": "test-key"},
		byProvider: map[string]ProviderConfig{
			"gateway": {
				ID: "gateway", BaseURL: server.URL,
			},
		},
	}

	if _, err := registry.DiscoverProviderModels(context.Background(), "gateway"); err == nil {
		t.Fatal("expected invalid catalog error")
	}
}

func TestDiscoverProviderModelsUsesConfiguredProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Errorf("request path = %s, want /v1/models", request.URL.Path)
		}
		if request.Host != "provider.invalid" {
			t.Errorf("request host = %s, want provider.invalid", request.Host)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer test-key" {
			t.Errorf("authorization = %q, want bearer key", authorization)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"id":"gpt-5"}]}`))
	}))
	defer proxy.Close()

	provider := ProviderConfig{
		ID: "gateway", BaseURL: "http://provider.invalid", ProxyURL: proxy.URL,
	}
	registry := &Registry{
		modelsPath:  filepath.Join(t.TempDir(), "models.json"),
		secretsPath: filepath.Join(t.TempDir(), "secrets.json"),
		secrets:     map[string]string{"gateway": "test-key"},
	}
	if err := registry.ApplyConfig(FileConfig{
		Providers: []ProviderConfig{provider},
		Models:    []ModelConfig{},
		Profiles:  map[string]ModelReference{},
	}); err != nil {
		t.Fatalf("apply provider configuration: %v", err)
	}

	models, err := registry.DiscoverProviderModels(context.Background(), "gateway")
	if err != nil {
		t.Fatalf("discover provider models: %v", err)
	}
	if want := []string{"gpt-5"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
}
