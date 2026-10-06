package jsonl

import (
	"strings"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// validateRestoredEvent 在恢复数据进入投影前拒绝非规范化字段，不修正已持久化的事实。
// 正常写入使用输入边界已规范化的值，不经过这项恢复检查。
func validateRestoredEvent(scope string, event Event) error {
	if event.Collection != "task" {
		return nil
	}
	task, err := decode[taskmodel.Task](event.Collection, object{scope: scope, data: event.Payload})
	if err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"task.id", task.ID.String()},
		{"task.sessionID", task.SessionID.String()},
		{"task.agentID", task.AgentID.String()},
		{"task.requestID", task.RequestID.String()},
		{"task.providerID", task.ProviderID},
		{"task.modelID", task.ModelID},
		{"task.reasoningLevel", task.ReasoningLevel},
		{"task.failureMessage", task.FailureMessage},
		{"task.input.agentDefinitionRevision", task.Input.AgentDefinitionRevision},
		{"task.input.contextDigest", task.Input.ContextDigest},
		{"task.input.currentInputMessageID", task.Input.CurrentInputMessageID},
	} {
		if field.value != strings.TrimSpace(field.value) {
			return contracts.InvalidValue(field.name, "恢复字段必须已规范化")
		}
	}
	return nil
}
