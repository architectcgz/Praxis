package streaming

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"praxis/internal/provider"
	"praxis/internal/provider/httpclient"
	"strings"
)

type EmitFunc func(provider.Event) error

const eventBufferCapacity = 16

type Decoder interface {
	Decode(context.Context, *SSEReader, EmitFunc) (provider.StopReason, error)
}

type DecoderFunc func(context.Context, *SSEReader, EmitFunc) (provider.StopReason, error)

func (f DecoderFunc) Decode(
	ctx context.Context,
	reader *SSEReader,
	emit EmitFunc,
) (provider.StopReason, error) {
	return f(ctx, reader, emit)
}

func Open(client *http.Client, request *http.Request) (*http.Response, error) {
	response, err := httpclient.RequestClient(client).Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, httpclient.DecodeErrorResponse(response)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		_ = response.Body.Close()
		return nil, errors.New("provider did not return text/event-stream")
	}
	return response, nil
}

// Start 异步将 SSE 响应解码为统一模型流事件；解码停止时关闭响应体和事件通道。
// 调用方提前停止处理时取消 ctx，并消费剩余事件直到通道关闭，以等待资源释放。
func Start(
	ctx context.Context,
	response *http.Response,
	decoder Decoder,
	sensitiveValues ...string,
) <-chan provider.Event {
	events := make(chan provider.Event, eventBufferCapacity)
	go func() {
		defer close(events)
		defer response.Body.Close()
		stopClose := context.AfterFunc(ctx, func() { _ = response.Body.Close() })
		defer stopClose()
		// 解码器通过 emit 转发事件，取消 context 后不再等待消费方。
		emit := func(event provider.Event) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case events <- event:
				return nil
			}
		}
		stopReason, err := decoder.Decode(ctx, NewSSEReader(response.Body), emit)
		_ = response.Body.Close()
		if err != nil {
			events <- provider.Event{Kind: provider.StreamError, Err: RedactError(err, sensitiveValues)}
			return
		}
		events <- provider.Event{
			Kind:       provider.StreamComplete,
			StopReason: stopReason,
		}
	}()
	return events
}

// RedactError 隐藏服务端错误中可能回显的协议秘密；保留错误链，取消与错误分类不受影响。
// sensitiveValues 由协议 adapter 提供，本层不认识签名或加密字段的结构。
func RedactError(err error, sensitiveValues []string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, value := range sensitiveValues {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[protocol state redacted]")
		}
	}
	if message == err.Error() {
		return err
	}
	return redactedError{cause: err, message: message}
}

type redactedError struct {
	cause   error
	message string
}

// Error 返回可安全展示的错误，不暴露原始载荷。
func (e redactedError) Error() string { return e.message }

// Unwrap 保留底层错误以供取消、超时和业务分类判断。
func (e redactedError) Unwrap() error { return e.cause }
