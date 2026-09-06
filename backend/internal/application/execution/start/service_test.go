package start

import (
	"context"
	"errors"
	"testing"

	commandprotocol "praxis/internal/command"
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
		var commandError *commandprotocol.Error
		if !errors.As(err, &commandError) || commandError.Code != commandprotocol.ErrorModelNotConfigured {
			t.Fatalf("SendInput() error = %v, want %s", err, commandprotocol.ErrorModelNotConfigured)
		}
	}
}
