package streaming

import (
	"context"
	"net/http"

	"praxis/internal/infra/providers"
	runtimecontract "praxis/internal/runtime"
)

type EmitFunc func(runtimecontract.ModelStreamEvent) error

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

// Start asynchronously decodes an SSE response into provider-neutral stream events.
// It closes response.Body and the returned channel when decoding stops. Callers that
// stop consuming before a terminal event must cancel ctx to unblock the decoder.
func Start(
	ctx context.Context,
	response *http.Response,
	decoder Decoder,
) <-chan runtimecontract.ModelStreamEvent {
	events := make(chan runtimecontract.ModelStreamEvent, eventBufferCapacity)
	go func() {
		defer close(events)
		defer response.Body.Close()
		// The decoder calls emit to forward each text, tool call, or other event to the consumer.
		emit := func(event runtimecontract.ModelStreamEvent) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case events <- event:
				return nil
			}
		}
		stopReason, err := decoder.Decode(ctx, NewSSEReader(response.Body), emit)
		if err != nil {
			_ = emit(runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamError, Err: err})
			return
		}
		_ = emit(runtimecontract.ModelStreamEvent{
			Kind:       runtimecontract.StreamComplete,
			StopReason: stopReason,
		})
	}()
	return events
}
