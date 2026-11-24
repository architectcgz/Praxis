package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// StreamTransport 限制响应体单次等待数据的时间，不限制连续到达数据的长流。
type StreamTransport struct {
	Transport   *http.Transport
	IdleTimeout time.Duration
}

func (t StreamTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	parent := request.Context()
	ctx, cancel := context.WithCancel(parent)
	response, err := t.Transport.RoundTrip(request.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &idleBody{
		ReadCloser: response.Body,
		parent:     parent,
		timeout:    t.IdleTimeout,
		cancel:     cancel,
	}
	return response, nil
}

func (t StreamTransport) CloseIdleConnections() { t.Transport.CloseIdleConnections() }

type idleBody struct {
	io.ReadCloser
	parent   context.Context
	timeout  time.Duration
	cancel   context.CancelFunc
	timedOut atomic.Bool
}

func (b *idleBody) Read(p []byte) (int, error) {
	// 取消本次 HTTP 请求以中断底层读取，不依赖响应体的 Close 是否等待 Read。
	timer := time.AfterFunc(b.timeout, func() {
		b.timedOut.Store(true)
		b.cancel()
	})
	n, err := b.ReadCloser.Read(p)
	timer.Stop()
	if err != nil && !errors.Is(err, io.EOF) && b.timedOut.Load() && b.parent.Err() == nil {
		return n, fmt.Errorf("provider response stream idle for %s: %w", b.timeout, os.ErrDeadlineExceeded)
	}
	return n, err
}

func (b *idleBody) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}

func DecodeErrorResponse(response *http.Response, maxBodyBytes int) error {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBodyBytes)+1))
	if err != nil {
		return fmt.Errorf(
			"provider request failed with HTTP %d: %s (read response body: %v)",
			response.StatusCode,
			http.StatusText(response.StatusCode),
			err,
		)
	}
	detail := strings.Join(strings.Fields(string(body)), " ")
	if len(detail) > maxBodyBytes {
		detail = detail[:maxBodyBytes] + "..."
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
