package streaming

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	runtimecontract "praxis/internal/runtime"
)

func TestSSEReaderCollectsMultilineData(t *testing.T) {
	reader := NewSSEReader(strings.NewReader(": keepalive\nevent: response.delta\nid: event-1\ndata: first\ndata: second\n\n"))
	event, err := reader.Next()
	if err != nil {
		t.Fatalf("read SSE event: %v", err)
	}
	if event.Type != "response.delta" || event.ID != "event-1" || event.Data != "first\nsecond" {
		t.Fatalf("unexpected SSE event: %#v", event)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("next SSE event error = %v, want EOF", err)
	}
}

func TestStartStopsBlockedEmitterAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &trackingBody{Reader: strings.NewReader("")}
	blocked := make(chan struct{})
	events := Start(ctx, &http.Response{Body: body}, DecoderFunc(func(_ context.Context, _ *SSEReader, emit EmitFunc) (string, error) {
		for index := 0; index < 17; index++ {
			if index == 16 {
				close(blocked)
			}
			if err := emit(runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamTextDelta}); err != nil {
				return "", err
			}
		}
		return "", nil
	}))
	<-blocked
	cancel()
	finished := make(chan struct{})
	go func() {
		for range events {
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("stream did not stop after context cancellation")
	}
	if !body.closed {
		t.Fatal("expected response body to close")
	}
}

func TestOpenRejectsNonSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := Open(nil, request)
	if response != nil {
		t.Fatalf("response = %#v, want nil", response)
	}
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("Open() error = %v, want HTTP status error", err)
	}
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}
