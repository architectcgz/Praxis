package streaming

import (
	"context"
	"net/http"

	"praxis/internal/core/model"
	"praxis/internal/infra/providers"
)

type EmitFunc func(model.ModelStreamEvent) error

const eventBufferCapacity = 16

type Decoder interface {
	Decode(context.Context, *SSEReader, EmitFunc) (string, error)
}

type DecoderFunc func(context.Context, *SSEReader, EmitFunc) (string, error)

func (f DecoderFunc) Decode(
	ctx context.Context,
	reader *SSEReader,
	emit EmitFunc,
) (string, error) {
	return f(ctx, reader, emit)
}

func Open(client *http.Client, request *http.Request) (*http.Response, error) {
	response, err := providers.RequestClient(client).Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, providers.DecodeErrorResponse(response)
	}
	return response, nil
}

// Start 异步将 SSE 响应解码为统一模型流事件；解码停止时关闭响应体和事件通道。
// 调用方在终态前停止消费时必须取消 ctx，避免解码协程阻塞。
func Start(
	ctx context.Context,
	response *http.Response,
	decoder Decoder,
) <-chan model.ModelStreamEvent {
	events := make(chan model.ModelStreamEvent, eventBufferCapacity)
	go func() {
		defer close(events)
		defer response.Body.Close()
		// 解码器通过 emit 转发事件，取消 context 后不再等待消费方。
		emit := func(event model.ModelStreamEvent) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case events <- event:
				return nil
			}
		}
		stopReason, err := decoder.Decode(ctx, NewSSEReader(response.Body), emit)
		if err != nil {
			_ = emit(model.ModelStreamEvent{Kind: model.StreamError, Err: err})
			return
		}
		_ = emit(model.ModelStreamEvent{
			Kind:       model.StreamComplete,
			StopReason: stopReason,
		})
	}()
	return events
}
