package entry

import (
	"encoding/json"
	"errors"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/core/session/entry/role"
	"strings"
)

// Validate 在内存接纳和持久化边界使用相同的结构校验，不修正原始正文。
func (m ContextEntry) Validate() error {
	if m.ID == "" || m.Sequence == 0 || m.CreatedAt.IsZero() {
		return errors.New("message identity, sequence and creation time are required")
	}
	return m.ValidateContent()
}

// ValidateContent 校验追加前的条目内容；序号和创建时间由存储分配。
func (m ContextEntry) ValidateContent() error {
	if m.ID == "" {
		return errors.New("message identity is required")
	}
	for _, value := range []string{m.ID, m.RequestID, m.SourceTaskID, m.HandoffID, m.ExecutionID, m.TurnID, m.AuthorID} {
		if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("message identity fields must be canonical")
		}
	}
	switch m.Role {
	case role.User, role.Assistant, role.Tool:
	default:
		return errors.New("message role is invalid")
	}
	switch m.AuthorKind {
	case AuthorUser, AuthorAgent, AuthorTool, AuthorSystem:
	default:
		return errors.New("message author is invalid")
	}
	if m.IsSummary() && (m.Role != role.User || m.AuthorKind != AuthorSystem) {
		return errors.New("summary entry requires user role and system author")
	}
	if len(m.Content) == 0 && !(m.Role == role.Assistant && (m.StopReason == "completed" || m.StopReason == "stop_sequence")) {
		return errors.New("message blocks are required")
	}
	if m.SourceTaskID != "" && (m.Role != role.User || m.ExecutionID == "") {
		return errors.New("task input requires a user message and execution identity")
	}
	seen := make(map[string]bool)
	for _, block := range m.Content {
		if block.Kind == BlockToolCall || block.Kind == BlockToolResult {
			if m.ExecutionID == "" || m.TurnID == "" || seen[block.CallID] {
				return errors.New("tool message ownership or call identity is invalid")
			}
			seen[block.CallID] = true
		}

		if err := validateBlock(m.Role, block); err != nil {
			return err
		}
	}
	return nil
}

func validateBlock(messageRole role.Role, block Block) error {
	if block.Kind != BlockReasoningState && block.ReasoningState != nil {
		return errors.New("reasoning state requires a reasoning state message block")
	}
	switch block.Kind {
	case BlockReasoningState:
		if messageRole != role.Assistant || block.ReasoningState == nil || block.Text != "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid reasoning state block")
		}
		return validateReasoningState(block.ReasoningState)
	case BlockText, BlockThinking:
		if block.Kind == BlockThinking && messageRole != role.Assistant || strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid text or thinking block")
		}
	case BlockToolCall, BlockToolResult:
		if block.CallID == "" || block.Name == "" || block.CallID != strings.TrimSpace(block.CallID) || block.Name != strings.TrimSpace(block.Name) {
			return errors.New("tool block requires canonical call identity and name")
		}
		if block.Kind == BlockToolCall && (messageRole != role.Assistant || block.IsError || len(block.Input) > 0 && !json.Valid(block.Input)) {
			return errors.New("message contains an invalid tool call block")
		}
		if block.Kind == BlockToolResult && (messageRole != role.Tool || len(block.Input) > 0) {
			return errors.New("message contains an invalid tool result block")
		}
	default:
		return errors.New("message contains an unknown block kind")
	}
	return nil
}

func validateReasoningState(state *ReasoningState) error {
	if state.ProviderID == "" || state.ProviderID != strings.TrimSpace(state.ProviderID) || state.ModelID == "" || state.ModelID != strings.TrimSpace(state.ModelID) {
		return errors.New("reasoning state requires canonical provider and model identities")
	}
	if state.Protocol != string(modelconfig.APIFormatOpenAIResponses) && state.Protocol != string(modelconfig.APIFormatAnthropicMessages) {
		return errors.New("unsupported reasoning state protocol")
	}
	if len(state.Payload) == 0 || len(state.Payload) > MaxReasoningStateBytes {
		return errors.New("invalid or oversized reasoning state payload")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(state.Payload, &object); err != nil || object == nil {
		return errors.New("reasoning state payload must be an object")
	}
	return nil
}
