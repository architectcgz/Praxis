package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	domaincontext "praxis/internal/domain/context"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"

	sessionport "praxis/internal/session"
)

type testModelResolver struct{ model ExecutionModel }

func (r testModelResolver) ResolveExecutionModel(domainsecurity.ModelSelection) (ExecutionModel, error) {
	return r.model, nil
}

type testStream struct {
	responses [][]ModelStreamEvent
	requests  []ModelRequest
	calls     int
}

func (s *testStream) Stream(_ context.Context, request ModelRequest) (<-chan ModelStreamEvent, error) {
	if s.calls >= len(s.responses) {
		return nil, errors.New("unexpected model turn")
	}
	response := s.responses[s.calls]
	s.calls++
	s.requests = append(s.requests, request)
	result := make(chan ModelStreamEvent, len(response))
	for _, event := range response {
		result <- event
	}
	close(result)
	return result, nil
}

type testToolInvoker struct {
	calls   int
	context ToolInvocationContext
}

func (i *testToolInvoker) Invoke(_ context.Context, _ ToolCall, invocationContext ToolInvocationContext) (ToolResult, error) {
	i.calls++
	i.context = invocationContext
	return ToolResult{Content: "tool-result"}, nil
}

type testTranscript struct {
	messages []sessionport.AgentSessionMessage
}

func (s *testTranscript) Initialize(context.Context, sessionport.AgentSessionHeader) error {
	return nil
}
func (s *testTranscript) AppendExecutionStart(context.Context, domainexecution.AgentExecution) (sessionport.ExecutionStartReceipt, error) {
	return sessionport.ExecutionStartReceipt{}, nil
}
func (s *testTranscript) AppendExecutionSettlement(context.Context, sessionport.ExecutionSettlementReceipt) (sessionport.ExecutionSettlementReceipt, error) {
	return sessionport.ExecutionSettlementReceipt{}, nil
}
func (s *testTranscript) AppendContextArtifact(context.Context, sessionport.ContextArtifact) (sessionport.ContextArtifactReceipt, error) {
	return sessionport.ContextArtifactReceipt{}, nil
}
func (s *testTranscript) FindExecutionStart(context.Context, domainfoundation.AgentExecutionID) (*sessionport.ExecutionStartReceipt, error) {
	return nil, nil
}
func (s *testTranscript) FindExecutionSettlement(context.Context, domainfoundation.AgentExecutionID) (*sessionport.ExecutionSettlementReceipt, error) {
	return nil, nil
}
func (s *testTranscript) FindContextArtifact(context.Context, domainfoundation.DeliveryID) (*sessionport.ContextArtifactReceipt, error) {
	return nil, nil
}
func (s *testTranscript) Repair(context.Context) (bool, error) { return false, nil }
func (s *testTranscript) Close(context.Context) error          { return nil }
func (s *testTranscript) ListMessages(context.Context, int) ([]sessionport.AgentSessionMessage, error) {
	return append([]sessionport.AgentSessionMessage(nil), s.messages...), nil
}
func (s *testTranscript) AppendStructuredMessage(_ context.Context, executionID domainfoundation.AgentExecutionID, messageID string, role string, _ domainfoundation.RequestID, blocks []sessionport.TranscriptContentBlock) error {
	var content string
	for _, block := range blocks {
		content += block.Text
	}
	s.messages = append(s.messages, sessionport.AgentSessionMessage{ExecutionID: executionID, MessageID: messageID, Role: role, Content: content, Blocks: blocks})
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
	return &testTranscript{messages: []sessionport.AgentSessionMessage{{
		ExecutionID: "execution_test", MessageID: "input:request_test", Role: "user", Content: "input",
		Blocks: []sessionport.TranscriptContentBlock{{Kind: "text", Text: "input"}},
	}}}
}

func TestExecutionEngineRejectsToolWithoutInvoker(t *testing.T) {
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

func TestExecutionEngineContinuesAfterToolResult(t *testing.T) {
	stream := &testStream{responses: [][]ModelStreamEvent{
		{{Kind: StreamToolCall, ToolCall: ToolCall{ID: "call", Name: domainsecurity.ToolReadFile}}, {Kind: StreamComplete}},
		{{Kind: StreamTextDelta, Text: "done"}, {Kind: StreamComplete}},
	}}
	invoker := &testToolInvoker{}
	transcript := transcriptWithInput()
	engine, err := NewExecutionEngine(ExecutionEngineConfig{
		Models:      testModelResolver{model: ExecutionModel{Stream: stream, ContextWindow: 4096, MaxOutputTokens: 128}},
		ToolInvoker: invoker,
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, code, err := engine.RunWithSession(context.Background(), testExecution(t, []domainsecurity.ToolName{domainsecurity.ToolReadFile}, domainsecurity.ResourceLimits{MaxTurns: 3, MaxToolCalls: 1}), transcript)
	if err != nil || outcome != domainexecution.ExecutionCompleted || code != "" {
		t.Fatalf("expected completed multi-turn execution, got outcome=%s code=%s err=%v", outcome, code, err)
	}
	if invoker.calls != 1 || stream.calls != 2 || len(transcript.messages) != 4 {
		t.Fatalf("expected assistant/tool/assistant transcript, calls=%d turns=%d messages=%d", invoker.calls, stream.calls, len(transcript.messages))
	}
	if invoker.context.ExecutionID != "execution_test" || invoker.context.SessionID != "session_test" || invoker.context.AgentID != "agent_test" {
		t.Fatalf("tool invocation context has incorrect ownership: %#v", invoker.context)
	}
	if transcript.messages[2].Role != string(TurnRoleTool) ||
		transcript.messages[2].Blocks[0].Kind != string(TurnContentToolResult) ||
		transcript.messages[3].Content != "done" {
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

type testToolCatalog struct{}

func (testToolCatalog) Definition(name domainsecurity.ToolName) (ToolDefinition, bool) {
	if name != domainsecurity.ToolListDir {
		return ToolDefinition{}, false
	}
	return ToolDefinition{
		Name: name, Description: "List a directory", InputSchema: json.RawMessage(`{"type":"object"}`),
	}, true
}

func (testToolCatalog) Normalize(ToolCall, ToolInvocationContext) (AuthorizedToolCall, error) {
	return AuthorizedToolCall{}, nil
}

func TestExecutionEngineExposesOnlyRegisteredGrantedTools(t *testing.T) {
	stream := &testStream{responses: [][]ModelStreamEvent{{
		{Kind: StreamTextDelta, Text: "done"}, {Kind: StreamComplete},
	}}}
	engine, err := NewExecutionEngine(ExecutionEngineConfig{
		Models: testModelResolver{model: ExecutionModel{Stream: stream, ContextWindow: 4096, MaxOutputTokens: 128}},
		Tools:  testToolCatalog{},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := []domainsecurity.ToolName{domainsecurity.ToolListDir, domainsecurity.ToolReadFile}
	outcome, _, err := engine.RunWithSession(
		context.Background(),
		testExecution(t, tools, domainsecurity.ResourceLimits{}),
		transcriptWithInput(),
	)
	if err != nil || outcome != domainexecution.ExecutionCompleted {
		t.Fatalf("RunWithSession() outcome=%s err=%v", outcome, err)
	}
	if len(stream.requests) != 1 || len(stream.requests[0].Snapshot.Tools) != 1 {
		t.Fatalf("unexpected model tools: %#v", stream.requests)
	}
	definition := stream.requests[0].Snapshot.Tools[0]
	if definition.Name != domainsecurity.ToolListDir || definition.Description == "" ||
		!json.Valid(definition.InputSchema) {
		t.Fatalf("invalid model-visible definition: %#v", definition)
	}
}

func TestExecutionEngineSetsExecutionTurnOutputTokenLimit(t *testing.T) {
	stream := &testStream{responses: [][]ModelStreamEvent{{{Kind: StreamComplete}}}}
	engine, err := NewExecutionEngine(ExecutionEngineConfig{
		Models: testModelResolver{model: ExecutionModel{Stream: stream, ContextWindow: 4096, MaxOutputTokens: 256}},
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, _, err := engine.RunWithSession(context.Background(), testExecution(t, nil, domainsecurity.ResourceLimits{}), transcriptWithInput())
	if err != nil || outcome != domainexecution.ExecutionCompleted {
		t.Fatalf("RunWithSession() outcome=%s err=%v", outcome, err)
	}
	if len(stream.requests) != 1 || stream.requests[0].Snapshot.MaxOutputTokens != 256 {
		t.Fatalf("unexpected execution turn snapshots: %#v", stream.requests)
	}
}
