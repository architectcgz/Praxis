package providers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func RequestClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

// NewProxyClient returns a copy of client that routes requests through raw.
// An empty proxy URL keeps the caller's existing client behavior.
func NewProxyClient(client *http.Client, raw string) (*http.Client, error) {
	_, proxyURL, err := parseProxyURL(raw)
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

func ValidateBaseURL(raw string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		return "", errors.New("provider base URL is required")
	}
	parsed, err := http.NewRequest(http.MethodGet, value, nil)
	if err != nil || parsed.URL == nil || parsed.URL.User != nil ||
		parsed.URL.RawQuery != "" || parsed.URL.Fragment != "" {
		return "", errors.New("provider base URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.URL.Scheme != "http" && parsed.URL.Scheme != "https" || parsed.URL.Host == "" {
		return "", errors.New("provider base URL must use http or https")
	}
	return value, nil
}

func ValidateProxyURL(raw string) (string, error) {
	value, _, err := parseProxyURL(raw)
	return value, err
}

func parseProxyURL(raw string) (string, *url.URL, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		return "", nil, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", nil, errors.New("provider proxy URL must be an absolute HTTP or HTTPS URL without a path, query, or fragment")
	}
	return value, parsed, nil
}

func DecodeErrorResponse(response *http.Response) error {
	defer response.Body.Close()
	// Provider response bodies are intentionally discarded: they may contain
	// authorization material or user prompt content.
	return fmt.Errorf(
		"provider request failed with HTTP %d: %s",
		response.StatusCode,
		http.StatusText(response.StatusCode),
	)
}
