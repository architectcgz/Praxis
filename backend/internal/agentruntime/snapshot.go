package agentruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"praxis/internal/core/domain"
)

// BuildTurnSnapshot reads the safe session projection and constructs the next immutable provider request.
func BuildTurnSnapshot(
	ctx context.Context,
	config RuntimeConfig,
	run domain.AgentRun,
	turnNumber int,
) (TurnSnapshot, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return TurnSnapshot{}, err
	}
	if err := validateRunForRuntime(run, normalized.AgentThreadID); err != nil {
		return TurnSnapshot{}, err
	}
	if run.Execution != normalized.Execution {
		return TurnSnapshot{}, &RuntimeError{
			Code:    ErrorContract,
			Message: "run execution snapshot does not match runtime config",
		}
	}
	if turnNumber < 1 {
		return TurnSnapshot{}, &RuntimeError{Code: ErrorContract, Message: "turn number must be positive"}
	}
	contextProjection, err := normalized.SessionStore.ReadContext(ctx, normalized.SessionReference)
	if err != nil {
		return TurnSnapshot{}, &RuntimeError{
			Code:    ErrorStorage,
			Message: "session context could not be read",
			Cause:   err,
		}
	}
	return TurnSnapshot{
		RunID:                run.ID,
		SessionReference:     normalized.SessionReference,
		Messages:             cloneTurnMessages(contextProjection.Messages),
		TaskPacket:           normalized.TaskPacket,
		ContextManifest:      normalized.ContextManifest,
		SystemPrompt:         normalized.Prompt.SystemPrompt,
		SystemPromptHash:     normalized.Prompt.SystemPromptHash,
		ArtifactTemplateHash: normalized.Prompt.ArtifactTemplateHash,
		Model:                normalized.Model,
		Tools:                cloneToolDefinitions(normalized.ToolDefinitions),
		GrantID:              normalized.Grant.ID,
		Execution:            normalized.Execution.Snapshot(),
		TurnNumber:           turnNumber,
	}, nil
}

func normalizeConfig(config RuntimeConfig) (RuntimeConfig, error) {
	if config.TaskSessionID == "" {
		config.TaskSessionID = config.Thread.TaskSessionID
	}
	if config.AgentThreadID == "" {
		config.AgentThreadID = config.Thread.ID
	}
	if err := config.validate(); err != nil {
		return RuntimeConfig{}, err
	}
	definitions, err := filterToolDefinitions(config.ToolDefinitions, config.Grant.AllowedTools)
	if err != nil {
		return RuntimeConfig{}, err
	}
	config.ToolDefinitions = definitions
	config.SessionReference = normalizeString(config.SessionReference)
	config.LeaseReference = normalizeString(config.LeaseReference)
	config.InjectionNonce = normalizeString(config.InjectionNonce)
	config.WrittenBy = normalizeString(config.WrittenBy)
	config.ToolDefinitionHashes = cloneStringMap(config.ToolDefinitionHashes)
	if config.MinimumReaderVersion == 0 {
		config.MinimumReaderVersion = 1
	}
	if config.InjectionNonce == "" {
		nonce, err := newInjectionNonce()
		if err != nil {
			return RuntimeConfig{}, err
		}
		config.InjectionNonce = nonce
	}
	config.Grant = config.Grant.Snapshot()
	config.Execution = config.Execution.Snapshot()
	return config, nil
}

// newInjectionNonce makes context-artifact wrappers unforgeable by workspace
// content while remaining durable in the session header for deterministic replay.
func newInjectionNonce() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session injection nonce: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func filterToolDefinitions(definitions []ToolDefinition, allowed []domain.ToolName) ([]ToolDefinition, error) {
	byName := make(map[domain.ToolName]ToolDefinition, len(definitions))
	for _, definition := range definitions {
		if !definition.Name.Valid() {
			return nil, &RuntimeError{Code: ErrorContract, Message: "tool definition has an unknown name"}
		}
		if _, exists := byName[definition.Name]; exists {
			return nil, &RuntimeError{Code: ErrorContract, Message: "duplicate tool definition"}
		}
		definition.Description = normalizeString(definition.Description)
		definition.InputSchema = cloneRaw(definition.InputSchema)
		byName[definition.Name] = definition
	}
	result := make([]ToolDefinition, 0, len(allowed))
	for _, tool := range allowed {
		definition, exists := byName[tool]
		if !exists {
			return nil, &RuntimeError{
				Code:    ErrorContract,
				Message: fmt.Sprintf("missing definition for granted tool %q", tool),
			}
		}
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func normalizeString(value string) string {
	return strings.TrimSpace(value)
}
