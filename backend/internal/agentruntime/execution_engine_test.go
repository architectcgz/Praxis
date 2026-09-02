package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	domaincontext "praxis/internal/core/domain/context"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"

	coresession "praxis/internal/core/session"
)

type testModelResolver struct{ model ExecutionModel }

func (r testModelResolver) ResolveExecutionModel(domainsecurity.ModelSelection) (ExecutionModel, error) {
	return r.model, nil
}

type testStream struct {
	responses [][]ModelStreamEvent
	calls     int
}

func (s *testStream) Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error) {
	if s.calls >= len(s.responses) {
		return nil, errors.New("unexpected model turn")
	}
	response := s.responses[s.calls]
	s.calls++
	result := make(chan ModelStreamEvent, len(response))
	for _, event := range response {
		result <- event
	}
	close(result)
	return result, nil
}

type testToolRunner struct{ calls int }

func (r *testToolRunner) Execute(context.Context, ToolCall, ToolExecutionContext) (ToolExecutionResult, error) {
	r.calls++
	return ToolExecutionResult{Content: "tool-result"}, nil
}

type testTranscript struct {
	messages []coresession.AgentSessionMessage
}

func (s *testTranscript) Initialize(context.Context, coresession.AgentSessionHeader) error {
	return nil
}
func (s *testTranscript) AppendExecutionStart(context.Context, domainexecution.AgentExecution) (coresession.ExecutionStartReceipt, error) {
	return coresession.ExecutionStartReceipt{}, nil
}
func (s *testTranscript) AppendExecutionSettlement(context.Context, coresession.ExecutionSettlementReceipt) (coresession.ExecutionSettlementReceipt, error) {
	return coresession.ExecutionSettlementReceipt{}, nil
}
func (s *testTranscript) AppendContextArtifact(context.Context, coresession.ContextArtifact) (coresession.ContextArtifactReceipt, error) {
	return coresession.ContextArtifactReceipt{}, nil
}
func (s *testTranscript) FindExecutionStart(context.Context, domainfoundation.AgentExecutionID) (*coresession.ExecutionStartReceipt, error) {
	return nil, nil
}
func (s *testTranscript) FindExecutionSettlement(context.Context, domainfoundation.AgentExecutionID) (*coresession.ExecutionSettlementReceipt, error) {
	return nil, nil
}
func (s *testTranscript) FindContextArtifact(context.Context, domainfoundation.DeliveryID) (*coresession.ContextArtifactReceipt, error) {
	return nil, nil
}
func (s *testTranscript) Repair(context.Context) (bool, error) { return false, nil }
func (s *testTranscript) Close(context.Context) error          { return nil }
func (s *testTranscript) ListMessages(context.Context, int) ([]coresession.AgentSessionMessage, error) {
	return append([]coresession.AgentSessionMessage(nil), s.messages...), nil
}
func (s *testTranscript) AppendStructuredMessage(_ context.Context, executionID domainfoundation.AgentExecutionID, messageID string, role string, _ domainfoundation.RequestID, blocks []coresession.TranscriptContentBlock) error {
	var content string
	for _, block := range blocks {
		content += block.Text
	}
	s.messages = append(s.messages, coresession.AgentSessionMessage{ExecutionID: executionID, MessageID: messageID, Role: role, Content: content, Blocks: blocks})
	return nil
}

func testExecution(t *testing.T, tools []domainsecurity.ToolName, limits domainsecurity.ResourceLimits) domainexecution.AgentExecution {
	t.Helper()
	grant, err := domainsecurity.NewCapabilityGrant(domainsecurity.CapabilityGrantSpec{
		ID: "grant_test", WorkspaceID: "workspace_test", WorkspacePathSnapshot: `C:\workspace`, WorkspaceRevision: 1,
		AllowedTools: tools, ReadScopes: []string{`C:\workspace`}, Model: domainsecurity.ModelSelection{ProviderID: "provider", ModelID: "model"},
		ResourceLimits: limits, ContextManifestRef: "manifest_test", ApprovalSource: domainsecurity.ApprovalSourcePolicyDefault,
		ApprovalPolicyFingerprint: "policy", CanProposeDelegation: containsToolForTest(tools, domainsecurity.ToolProposeDelegate),
		ResultPermissions: resultPermissionsForTest(tools),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := domaincontext.NewContextManifest("manifest_test", "context", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	runtimeSnapshot, err := domainexecution.NewRuntimeExecutionSnapshot(domainsecurity.SandboxReadOnly, domainsecurity.ApprovalAlwaysAsk, "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	input := domainexecution.ExecutionInputSnapshot{
		ContextManifest: manifest, ContextSelection: domainexecution.ContextSelection{Revision: 1, Summary: "context"},
		Security: domainsecurity.ExecutionSecuritySnapshot{AgentPolicyRevision: 1, CapabilityGrant: grant, Sandbox: domainsecurity.SandboxConstraints{Mode: domainsecurity.SandboxReadOnly}, ApprovalRules: []domainsecurity.ApprovalRule{{Mode: domainsecurity.ApprovalAlwaysAsk}}, Fingerprint: "fingerprint"},
		Runtime:  runtimeSnapshot,
	}
	execution, err := domainexecution.NewAgentExecution("execution_test", "session_test", "agent_test", "request_test", domainexecution.ExecutionUserInput, "goal", input, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return execution
}

func transcriptWithInput() *testTranscript {
	return &testTranscript{messages: []coresession.AgentSessionMessage{{
		ExecutionID: "execution_test", MessageID: "input:request_test", Role: "user", Content: "input",
		Blocks: []coresession.TranscriptContentBlock{{Kind: "text", Text: "input"}},
	}}}
}

func TestExecutionEngineRejectsToolWithoutRunner(t *testing.T) {
	stream := &testStream{responses: [][]ModelStreamEvent{{{Kind: StreamToolCall, ToolCall: ToolCall{ID: "call", Name: domainsecurity.ToolReadFile, Input: json.RawMessage(`{"path":"C:\\workspace\\a"}`)}}, {Kind: StreamComplete}}}}
	engine, err := NewExecutionEngine(ExecutionEngineConfig{Models: testModelResolver{model: ExecutionModel{Stream: stream, ContextWindow: 4096, MaxOutputTokens: 128}}})
	if err != nil {
		t.Fatal(err)
	}
	outcome, code, err := engine.RunWithSession(context.Background(), testExecution(t, []domainsecurity.ToolName{domainsecurity.ToolReadFile}, domainsecurity.ResourceLimits{MaxTurns: 2}), transcriptWithInput())
	if outcome != domainexecution.ExecutionFailed || code != domainexecution.ExecutionFailureTool || !IsCode(err, ErrorTool) {
		t.Fatalf("expected stable tool failure, got outcome=%s code=%s err=%v", outcome, code, err)
	}
}

func TestExecutionEngineRejectsUnauthorizedTool(t *testing.T) {
	stream := &testStream{responses: [][]ModelStreamEvent{{{Kind: StreamToolCall, ToolCall: ToolCall{ID: "call", Name: domainsecurity.ToolReadFile}}, {Kind: StreamComplete}}}}
	engine, err := NewExecutionEngine(ExecutionEngineConfig{Models: testModelResolver{model: ExecutionModel{Stream: stream, ContextWindow: 4096, MaxOutputTokens: 128}}, Tools: &testToolRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	outcome, code, err := engine.RunWithSession(context.Background(), testExecution(t, []domainsecurity.ToolName{domainsecurity.ToolListDir}, domainsecurity.ResourceLimits{MaxTurns: 2}), transcriptWithInput())
	if outcome != domainexecution.ExecutionFailed || code != domainexecution.ExecutionFailurePolicyBlocked || !IsCode(err, ErrorPolicyBlocked) {
		t.Fatalf("expected policy failure, got outcome=%s code=%s err=%v", outcome, code, err)
	}
}

func TestExecutionEngineContinuesAfterToolResult(t *testing.T) {
	stream := &testStream{responses: [][]ModelStreamEvent{
		{{Kind: StreamToolCall, ToolCall: ToolCall{ID: "call", Name: domainsecurity.ToolReadFile}}, {Kind: StreamComplete}},
		{{Kind: StreamTextDelta, Text: "done"}, {Kind: StreamComplete}},
	}}
	runner := &testToolRunner{}
	transcript := transcriptWithInput()
	engine, err := NewExecutionEngine(ExecutionEngineConfig{Models: testModelResolver{model: ExecutionModel{Stream: stream, ContextWindow: 4096, MaxOutputTokens: 128}}, Tools: runner})
	if err != nil {
		t.Fatal(err)
	}
	outcome, code, err := engine.RunWithSession(context.Background(), testExecution(t, []domainsecurity.ToolName{domainsecurity.ToolReadFile}, domainsecurity.ResourceLimits{MaxTurns: 3, MaxToolCalls: 1}), transcript)
	if err != nil || outcome != domainexecution.ExecutionCompleted || code != "" {
		t.Fatalf("expected completed multi-turn execution, got outcome=%s code=%s err=%v", outcome, code, err)
	}
	if runner.calls != 1 || stream.calls != 2 || len(transcript.messages) != 4 {
		t.Fatalf("expected assistant/tool/assistant transcript, calls=%d turns=%d messages=%d", runner.calls, stream.calls, len(transcript.messages))
	}
	if transcript.messages[2].Blocks[0].Kind != string(TurnContentToolResult) || transcript.messages[3].Content != "done" {
		t.Fatalf("tool result was not persisted before the next turn: %#v", transcript.messages)
	}
}

func containsToolForTest(tools []domainsecurity.ToolName, target domainsecurity.ToolName) bool {
	for _, tool := range tools {
		if tool == target {
			return true
		}
	}
	return false
}

func resultPermissionsForTest(tools []domainsecurity.ToolName) []domainsecurity.ResultPermission {
	result := make([]domainsecurity.ResultPermission, 0)
	if containsToolForTest(tools, domainsecurity.ToolSubmitResult) {
		result = append(result, domainsecurity.ResultPermissionAgentResult)
	}
	if containsToolForTest(tools, domainsecurity.ToolSubmitBriefing) {
		result = append(result, domainsecurity.ResultPermissionBriefing)
	}
	return result
}
