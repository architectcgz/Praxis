package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	modelconfig "praxis/internal/core/model/config"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"reflect"
	"slices"
	"strings"
)

// validateSession 在磁盘输入边界拒绝非法会话，不修改持久化字段。
func validateSession(value sessionmodel.Session) error {
	for _, id := range []string{
		value.ID.String(),
		value.ProjectID.String(),
		value.WorkspaceID.String(),
		value.MainAgentID.String(),
	} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("session: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if !definitionIDPattern.MatchString(value.AgentDefinitionID.String()) {
		return fmt.Errorf("session.agentDefinitionID: invalid agent definition identity: %w", errorcode.ErrInvalidValue)
	}
	if value.State != sessionmodel.SessionActive && value.State != sessionmodel.SessionArchived {
		return fmt.Errorf("session.state: unknown session state: %w", errorcode.ErrInvalidValue)
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return fmt.Errorf("session.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

// validateReasoningState 在消息输入边界检查存储字段，不解释 Provider 的 opaque 内容。
func validateReasoningState(state *sessionentry.ReasoningState) error {
	if state == nil {
		return errors.New("reasoning state is required")
	}
	if state.ProviderID == "" || state.ProviderID != strings.TrimSpace(state.ProviderID) {
		return errors.New("reasoning state provider ID is required and must be canonical")
	}
	if state.ModelID == "" || state.ModelID != strings.TrimSpace(state.ModelID) {
		return errors.New("reasoning state model ID is required and must be canonical")
	}
	if state.Protocol != string(modelconfig.APIFormatOpenAIResponses) && state.Protocol != string(modelconfig.APIFormatAnthropicMessages) {
		return errors.New("unsupported reasoning state protocol")
	}
	if len(state.Payload) == 0 || len(state.Payload) > sessionentry.MaxReasoningStateBytes {
		return errors.New("invalid or oversized reasoning state payload")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(state.Payload, &object); err != nil || object == nil {
		return errors.New("reasoning state payload must be an object")
	}
	return nil
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		return values
	}
	return values[:min(len(values), limit)]
}
func same(a, b any) bool { return reflect.DeepEqual(a, b) }
func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
