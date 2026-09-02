package orchestrate

import (
	"context"
	"errors"
	"testing"
)

func TestSendInputRequiresExplicitModelSelection(t *testing.T) {
	orchestrator := &AgentOrchestrator{ready: true}

	for _, request := range []SendInputRequest{
		{SessionID: "session", RequestID: "request", Content: "hello", ModelID: "model"},
		{SessionID: "session", RequestID: "request", Content: "hello", ProviderID: "provider"},
	} {
		_, err := orchestrator.SendInput(context.Background(), request)
		var command *CommandError
		if !errors.As(err, &command) || command.Code != CommandErrorModelNotConfigured {
			t.Fatalf("SendInput() error = %v, want %s", err, CommandErrorModelNotConfigured)
		}
	}
}
