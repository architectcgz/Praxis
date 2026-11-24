package bootstrap

import (
	"context"
	"net"
	"net/http"
	"praxis/internal/config"
	"praxis/internal/engine/session"
	"praxis/internal/logging"
	"praxis/internal/provider/httpclient"
	"praxis/internal/provider/router"
	"praxis/internal/provider/streaming"
	"praxis/internal/tools"
)

// configuration 持有固定的工具实现和进程策略，每次加载返回独立的会话快照。
type configuration struct {
	registry *tools.ToolRegistry
	model    config.ModelRuntime
	logger   *logging.Logger
}

func loadConfiguration(ctx context.Context, root config.DataRoot, runtime config.Runtime, logger *logging.Logger, extensions []tools.Tool) (*configuration, error) {
	registry, err := buildToolRegistry(runtime.Tools, extensions)
	if err != nil {
		return nil, err
	}
	loader := &configuration{
		registry: registry,
		model:    runtime.Model,
		logger:   logger,
	}
	if _, err := loader.loadSessionConfiguration(ctx, root); err != nil {
		return nil, err
	}
	return loader, nil
}

func (c *configuration) loadSessionConfiguration(ctx context.Context, root config.DataRoot) (*session.Configuration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	settings, err := config.LoadSettings(root.SettingsFile)
	if err != nil {
		return nil, err
	}
	models, err := config.LoadModels(root.ModelProvidersConfig, root.ModelCredentialsFile)
	if err != nil {
		return nil, err
	}
	definitions, err := config.LoadAgentDefinitions(root.AgentDefinitions)
	if err != nil {
		return nil, err
	}
	tools, err := config.LoadTools(root.ToolConfig, c.registry)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &session.Configuration{
		Models:      models.Catalog(),
		Definitions: definitions,
		Tools:       tools,
		Provider: router.New(router.Dependencies{
			Credentials: models.Credentials(),
			HTTPClients: buildProviderClients(models.Proxies(), c.model),
			Warnf:       c.logger.Warnf,
			StreamLimits: streaming.Limits{
				MaxFrameBytes:       c.model.Stream.MaxFrameBytes,
				EventBufferCapacity: c.model.Stream.EventBufferCapacity,
				MaxErrorBodyBytes:   c.model.Stream.MaxErrorBodyBytes,
			},
		}),
		GlobalApprovalMode: settings.ApprovalMode,
	}, nil
}

func buildProviderClients(proxies map[string]config.Proxy, policy config.ModelRuntime) map[string]*http.Client {
	clients := make(map[string]*http.Client, len(proxies))
	dialer := net.Dialer{
		Timeout:   policy.ConnectTimeout,
		KeepAlive: policy.ConnectionKeepAlive,
	}
	for id, proxy := range proxies {
		transport := &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			IdleConnTimeout:       policy.IdleConnectionTimeout,
			TLSHandshakeTimeout:   policy.ConnectTimeout,
			ResponseHeaderTimeout: policy.ResponseHeaderTimeout,
		}
		switch {
		case proxy.FromEnvironment:
			transport.Proxy = http.ProxyFromEnvironment
		case proxy.URL != nil:
			transport.Proxy = http.ProxyURL(proxy.URL)
		}
		clients[id] = &http.Client{Transport: httpclient.StreamTransport{
			Transport:   transport,
			IdleTimeout: policy.StreamIdleTimeout,
		}}
	}
	return clients
}
