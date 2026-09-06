package start

import (
	"context"
	"errors"
	"testing"

	corecommand "praxis/internal/core/command"
)

type readyReadiness bool

func (r readyReadiness) Ready() bool {
	return bool(r)
}

func TestSendInputRequiresExplicitModelSelection(t *testing.T) {
	service := &Service{readiness: readyReadiness(true)}
	for _, params := range []SendInputParams{
		{SessionID: "session", RequestID: "request", Content: "hello", ModelID: "model"},
		{SessionID: "session", RequestID: "request", Content: "hello", ProviderID: "provider"},
	} {
		_, err := service.SendInput(context.Background(), params)
		var commandError *corecommand.Error
		if !errors.As(err, &commandError) || commandError.Code != corecommand.ErrorModelNotConfigured {
			t.Fatalf("SendInput() error = %v, want %s", err, corecommand.ErrorModelNotConfigured)
		}
	}
}
