package runtimetest

import (
	"context"
	"sync"

	"praxis/internal/agentruntime"
)

// ScriptedModelResponse describes one deterministic model request.
type ScriptedModelResponse struct {
	Events []agentruntime.ModelStreamEvent
	Err    error
}

// ScriptedModel is a thread-safe faux model for runtime lifecycle verification.
type ScriptedModel struct {
	mu        sync.Mutex
	responses []ScriptedModelResponse
	requests  []agentruntime.ModelRequest
}

// NewScriptedModel creates a model that consumes responses in order.
func NewScriptedModel(responses ...ScriptedModelResponse) *ScriptedModel {
	return &ScriptedModel{responses: cloneResponses(responses)}
}

// AddResponse appends a response for a future request.
func (m *ScriptedModel) AddResponse(response ScriptedModelResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, cloneResponse(response))
}

// Requests returns defensive copies of all provider requests.
func (m *ScriptedModel) Requests() []agentruntime.ModelRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]agentruntime.ModelRequest, len(m.requests))
	for i, request := range m.requests {
		result[i] = request
		result[i].Snapshot = request.Snapshot.Snapshot()
	}
	return result
}

// Stream implements agentruntime.ModelStreamPort.
func (m *ScriptedModel) Stream(
	ctx context.Context,
	request agentruntime.ModelRequest,
) (<-chan agentruntime.ModelStreamEvent, error) {
	m.mu.Lock()
	m.requests = append(m.requests, request)
	var response ScriptedModelResponse
	if len(m.responses) > 0 {
		response = m.responses[0]
		m.responses = m.responses[1:]
	}
	m.mu.Unlock()
	stream := make(chan agentruntime.ModelStreamEvent, len(response.Events))
	go func() {
		defer close(stream)
		for _, event := range response.Events {
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	if response.Err != nil {
		return nil, response.Err
	}
	return stream, nil
}

func cloneResponses(responses []ScriptedModelResponse) []ScriptedModelResponse {
	result := make([]ScriptedModelResponse, len(responses))
	for i, response := range responses {
		result[i] = cloneResponse(response)
	}
	return result
}

func cloneResponse(response ScriptedModelResponse) ScriptedModelResponse {
	response.Events = append([]agentruntime.ModelStreamEvent(nil), response.Events...)
	return response
}
