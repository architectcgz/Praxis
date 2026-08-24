package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// KeyResolver resolves a named secret at request time. Provider adapters never
// read environment variables or secret files directly.
type KeyResolver func(context.Context, string) (string, error)

var envNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// EnvironmentKeyResolver is the default resolver for deployments that only
// provide credentials through the process environment.
func EnvironmentKeyResolver(ctx context.Context, name string) (string, error) {
	if ctx == nil {
		return "", errors.New("key resolver context is required")
	}
	if !envNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid API key environment name %q", name)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("API key is not configured for %s", name)
	}
	return value, nil
}

func RequestClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
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
