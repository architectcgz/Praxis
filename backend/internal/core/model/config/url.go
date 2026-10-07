package config

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ValidateBaseURL 只读校验 canonical 模型端点，不隐式修改被使用的 URL。
// 地址必须使用 HTTP 或 HTTPS，不能包含凭据、查询参数或片段。
func ValidateBaseURL(value string) (string, error) {
	if value == "" {
		return "", errors.New("provider base URL is required")
	}
	if !canonicalString(value) || strings.HasSuffix(value, "/") {
		return "", errors.New("provider base URL must be normalized")
	}
	parsed, err := http.NewRequest(http.MethodGet, value, nil)
	if err != nil || parsed.URL == nil || parsed.URL.User != nil || parsed.URL.RawQuery != "" || parsed.URL.Fragment != "" {
		return "", errors.New("provider base URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.URL.Scheme != "http" && parsed.URL.Scheme != "https" || parsed.URL.Host == "" {
		return "", errors.New("provider base URL must use http or https")
	}
	return value, nil
}

// ValidateProxyURL 只读校验 canonical 代理地址；空值表示不覆盖 HTTP client 的代理行为。
func ValidateProxyURL(value string) (string, error) {
	if _, err := ParseProxyURL(value); err != nil {
		return "", err
	}
	return value, nil
}

// ParseProxyURL 校验并解析 canonical 代理地址，空值返回 nil。
// 代理地址允许认证信息，但不能带路径、查询参数或片段；失败不清洗输入。
func ParseProxyURL(value string) (*url.URL, error) {
	if value == "" {
		return nil, nil
	}
	if !canonicalString(value) {
		return nil, errors.New("provider proxy URL must be normalized")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("provider proxy URL must be an absolute HTTP or HTTPS URL without a path, query, or fragment")
	}
	return parsed, nil
}
