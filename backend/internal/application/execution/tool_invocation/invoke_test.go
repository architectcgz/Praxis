package toolinvocation

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

type immediateTx struct{}

func (immediateTx) InTx(ctx context.Context, run func(context.Context) error) error { return run(ctx) }

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fixedIDs struct{}

func (fixedIDs) New(prefix string) string { return prefix + "_test" }

type executionReader struct {
	value domainexecution.AgentExecution
}

func (r executionReader) Get(
	context.Context,
	domainfoundation.AgentExecutionID,
) (domainexecution.AgentExecution, error) {
	return r.value, nil
}

type securityReader struct {
	value domainsecurity.ExecutionSecuritySnapshot
}

func (r securityReader) Get(
	context.Context,
	domainfoundation.AgentExecutionID,
) (domainsecurity.ExecutionSecuritySnapshot, error) {
	return r.value, nil
}

type memoryInvocations struct {
	values map[domainfoundation.ToolInvocationID]domainexecution.ToolInvocation
}

func newMemoryInvocations() *memoryInvocations {
	return &memoryInvocations{values: make(map[domainfoundation.ToolInvocationID]domainexecution.ToolInvocation)}
}

func (r *memoryInvocations) Get(
	_ context.Context,
	id domainfoundation.ToolInvocationID,
) (domainexecution.ToolInvocation, error) {
	value, ok := r.values[id]
	if !ok {
		return domainexecution.ToolInvocation{}, domainfoundation.ErrNotFound
	}
	return value.Snapshot(), nil
}

func (r *memoryInvocations) FindByExecutionCall(
	_ context.Context,
	executionID domainfoundation.AgentExecutionID,
	callID string,
) (domainexecution.ToolInvocation, error) {
	for _, value := range r.values {
		if value.ExecutionID == executionID && value.ProviderToolCallID == callID {
			return value.Snapshot(), nil
		}
	}
	return domainexecution.ToolInvocation{}, domainfoundation.ErrNotFound
}

func (r *memoryInvocations) Save(_ context.Context, value domainexecution.ToolInvocation) error {
	r.values[value.ID] = value.Snapshot()
	return nil
}

type testCatalog struct{}

func (testCatalog) Definition(name domainsecurity.ToolName) (runtimecontract.ToolDefinition, bool) {
	return runtimecontract.ToolDefinition{
		Name: name, InputSchema: json.RawMessage(`{"type":"object"}`),
	}, name == domainsecurity.ToolListDir
}

func (testCatalog) Normalize(
	call runtimecontract.ToolCall,
	invocation runtimecontract.ToolInvocationContext,
) (runtimecontract.AuthorizedToolCall, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Input, &input); err != nil || input.Path == "" {
		return runtimecontract.AuthorizedToolCall{}, errors.New("invalid arguments")
	}
	path := input.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(invocation.Grant.WorkspacePathSnapshot, path)
	}
	path = filepath.Clean(path)
	normalized, _ := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: path})
	return runtimecontract.AuthorizedToolCall{Name: call.Name, NormalizedArguments: normalized, Path: path}, nil
}

type countingExecutor struct {
	calls int
}

func (e *countingExecutor) Execute(
	context.Context,
	runtimecontract.AuthorizedToolCall,
) (runtimecontract.ToolResult, error) {
	e.calls++
	return runtimecontract.ToolResult{Content: `{"entries":[]}`}, nil
}

func TestInvokePersistsResultAndDoesNotReplayDuplicateCall(t *testing.T) {
	service, invocationContext, repository, executor := newServiceFixture(t)
	call := runtimecontract.ToolCall{
		ID: "call-1", Name: domainsecurity.ToolListDir, Input: json.RawMessage(`{"path":"src"}`),
	}
	first, err := service.Invoke(context.Background(), call, invocationContext)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Invoke(context.Background(), call, invocationContext)
	if err != nil {
		t.Fatal(err)
	}
	if first.Content != second.Content || executor.calls != 1 {
		t.Fatalf("duplicate call replayed: calls=%d first=%q second=%q", executor.calls, first.Content, second.Content)
	}
	invocation, err := repository.FindByExecutionCall(context.Background(), invocationContext.ExecutionID, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Status != domainexecution.ToolInvocationSucceeded || invocation.Result.InlineContent != first.Content {
		t.Fatalf("unexpected persisted invocation: %#v", invocation)
	}
}

func TestInvokeDeniesPathOutsideGrantBeforeExecution(t *testing.T) {
	service, invocationContext, repository, executor := newServiceFixture(t)
	outside := filepath.Join(filepath.Dir(invocationContext.Grant.WorkspacePathSnapshot), "outside")
	call := runtimecontract.ToolCall{
		ID: "call-1", Name: domainsecurity.ToolListDir,
		Input: json.RawMessage(`{"path":` + quoted(outside) + `}`),
	}
	result, err := service.Invoke(context.Background(), call, invocationContext)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorClass != string(domainexecution.ToolFailureNotAllowed) || executor.calls != 0 {
		t.Fatalf("unauthorized call result=%#v executor calls=%d", result, executor.calls)
	}
	invocation, err := repository.FindByExecutionCall(context.Background(), invocationContext.ExecutionID, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Status != domainexecution.ToolInvocationDenied {
		t.Fatalf("status=%s want=%s", invocation.Status, domainexecution.ToolInvocationDenied)
	}
}

func TestInvokeRejectsReusedCallIdentityWithDifferentArguments(t *testing.T) {
	service, invocationContext, _, executor := newServiceFixture(t)
	first := runtimecontract.ToolCall{
		ID: "call-1", Name: domainsecurity.ToolListDir, Input: json.RawMessage(`{"path":"src"}`),
	}
	if _, err := service.Invoke(context.Background(), first, invocationContext); err != nil {
		t.Fatal(err)
	}
	conflict := runtimecontract.ToolCall{
		ID: "call-1", Name: domainsecurity.ToolListDir, Input: json.RawMessage(`{"path":"docs"}`),
	}
	result, err := service.Invoke(context.Background(), conflict, invocationContext)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorClass != "tool_request_conflict" || executor.calls != 1 {
		t.Fatalf("conflict result=%#v executor calls=%d", result, executor.calls)
	}
}

func newServiceFixture(
	t *testing.T,
) (*Service, runtimecontract.ToolInvocationContext, *memoryInvocations, *countingExecutor) {
	t.Helper()
	root := filepath.Clean(t.TempDir())
	grant := domainsecurity.CapabilityGrant{
		ID: "grant_test", WorkspacePathSnapshot: root,
		AllowedTools: []domainsecurity.ToolName{domainsecurity.ToolListDir}, ReadScopes: []string{root},
	}
	security := domainsecurity.ExecutionSecuritySnapshot{CapabilityGrant: grant, Fingerprint: "security_test"}
	execution := domainexecution.AgentExecution{
		ID: "execution_test", SessionID: "session_test", AgentID: "agent_test",
		Status: domainexecution.ExecutionRunning,
		Input: domainexecution.ExecutionInputSnapshot{
			Security: security, Runtime: domainexecution.RuntimeExecutionSnapshot{Revision: security.Fingerprint},
		},
	}
	repository := newMemoryInvocations()
	executor := &countingExecutor{}
	service, err := NewService(Config{
		Transactions: immediateTx{}, Executions: executionReader{value: execution},
		SecuritySnapshots: securityReader{value: security}, Invocations: repository,
		Catalog: testCatalog{}, Executor: executor,
		Clock: fixedClock{at: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}, IDs: fixedIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	invocationContext := runtimecontract.ToolInvocationContext{
		ExecutionID: execution.ID, SessionID: execution.SessionID, AgentID: execution.AgentID,
		Grant: grant, Execution: execution.Input.Runtime,
	}
	return service, invocationContext, repository, executor
}

func quoted(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
