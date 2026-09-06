package app

import (
	"testing"

	domainfoundation "praxis/internal/domain/foundation"
	sessionport "praxis/internal/session"
)

func TestPublicAgentMessageHidesToolProtocolContent(t *testing.T) {
	toolResult := sessionport.AgentSessionMessage{
		Sequence: 1, ExecutionID: domainfoundation.AgentExecutionID("execution"), Role: "user",
		Content: `{"entries":[]}`,
		Blocks: []sessionport.TranscriptContentBlock{{
			Kind: "tool_result", ToolCallID: "call-1", ToolName: "list_dir", Text: `{"entries":[]}`,
		}},
	}
	if _, visible := publicAgentMessage(toolResult); visible {
		t.Fatal("tool result was exposed as a user message")
	}
	assistant := sessionport.AgentSessionMessage{
		Sequence: 2, ExecutionID: domainfoundation.AgentExecutionID("execution"), Role: "assistant",
		Content: "I will inspect the directory.[tool:list_dir]",
		Blocks: []sessionport.TranscriptContentBlock{
			{Kind: "text", Text: "I will inspect the directory."},
			{Kind: "tool_use", ToolCallID: "call-1", ToolName: "list_dir"},
		},
	}
	visible, ok := publicAgentMessage(assistant)
	if !ok || visible.Content != "I will inspect the directory." || visible.Role != "assistant" {
		t.Fatalf("unexpected assistant projection: %#v visible=%t", visible, ok)
	}
}
