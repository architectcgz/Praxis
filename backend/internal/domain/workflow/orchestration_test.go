package workflow

import (
	"testing"
	"time"
)

func TestAgentControlCommandLifecycle(t *testing.T) {
	at := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	command, err := NewAgentControlCommand("control_1", "agent_1", "execution_1", AgentControlPause, at)
	if err != nil {
		t.Fatal(err)
	}
	if command.Status != AgentControlPending || command.Kind != AgentControlPause {
		t.Fatalf("new command = %#v", command)
	}
	if err := command.MarkApplied(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if command.Status != AgentControlApplied || command.AppliedAt.IsZero() {
		t.Fatalf("applied command = %#v", command)
	}
	if err := command.MarkApplied(at.Add(2 * time.Second)); err == nil {
		t.Fatal("applying an applied command should fail")
	}
	if _, err := NewAgentControlCommand("", "agent_1", "", AgentControlPause, at); err == nil {
		t.Fatal("missing command id should be rejected")
	}
	if _, err := NewAgentControlCommand("control_2", "agent_1", "", AgentControlKind("bad"), at); err == nil {
		t.Fatal("unknown control kind should be rejected")
	}
}
