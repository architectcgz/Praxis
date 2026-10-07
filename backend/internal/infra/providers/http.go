package providers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	modelconfig "praxis/internal/core/model/config"
)

const maxProviderErrorBodyBytes = 16 << 10

func RequestClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

// NewProxyClient 根据 canonical 代理地址创建 HTTP client 副本，不再次清洗配置。
// 空地址保留调用方 client 的现有代理行为。
func NewProxyClient(client *http.Client, raw string) (*http.Client, error) {
	proxyURL, err := modelconfig.ParseProxyURL(raw)
	if err != nil {
		return nil, err
	}
	base := RequestClient(client)
	if proxyURL == nil {
		return base, nil
	}
	transport := http.DefaultTransport.(*http.Transport)
	if base.Transport != nil {
		configured, ok := base.Transport.(*http.Transport)
		if !ok {
			return nil, errors.New("provider proxy requires a standard HTTP transport")
		}
		transport = configured
	}
	copy := *base
	proxyTransport := transport.Clone()
	proxyTransport.Proxy = http.ProxyURL(proxyURL)
	copy.Transport = proxyTransport
	return &copy, nil
}

func DecodeErrorResponse(response *http.Response) error {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderErrorBodyBytes+1))
	if err != nil {
		return fmt.Errorf(
			"provider request failed with HTTP %d: %s (read response body: %v)",
			response.StatusCode,
			http.StatusText(response.StatusCode),
			err,
		)
	}
	detail := strings.Join(strings.Fields(string(body)), " ")
	if len(detail) > maxProviderErrorBodyBytes {
		detail = detail[:maxProviderErrorBodyBytes] + "..."
	}
	if detail != "" {
		return fmt.Errorf(
			"provider request failed with HTTP %d: %s: %s",
			response.StatusCode,
			http.StatusText(response.StatusCode),
			detail,
		)
	}
	return fmt.Errorf(
		"provider request failed with HTTP %d: %s",
		response.StatusCode,
		http.StatusText(response.StatusCode),
	)
}
