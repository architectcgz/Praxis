package app

import (
	"testing"

	"praxis/internal/contracts"
)

func TestSendInputRequiresExplicitModelSelection(t *testing.T) {
	application := New()

	for _, request := range []contracts.SendInputRequest{
		{ModelID: "model"},
		{ProviderID: "provider"},
	} {
		_, err := application.commands.SendInput(request)
		if err == nil || err.Error() != contracts.ErrorCodeModelNotConfigured.String() {
			t.Fatalf("SendInput() error = %v, want %s", err, contracts.ErrorCodeModelNotConfigured)
		}
	}
}
