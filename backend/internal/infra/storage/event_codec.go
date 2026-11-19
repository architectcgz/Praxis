package storage

import (
	"encoding/json"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/identity"
)

// storedEntry 只附加日志归属信息，不复用前端通知信封。
type storedEntry struct {
	AgentID identity.AgentID   `json:"agent_id"`
	Entry   sessionentry.Entry `json:"entry"`
}

func encodeEntry(agentID identity.AgentID, entry sessionentry.Entry) ([]byte, error) {
	data, err := json.Marshal(storedEntry{AgentID: agentID, Entry: entry})
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeEntry(data []byte) (storedEntry, error) {
	var value storedEntry
	if err := decodeJSON(data, &value); err != nil {
		return value, fmt.Errorf("decode history entry: %w", err)
	}
	if value.AgentID == "" || value.Entry.ID == "" || value.Entry.RunID == "" || value.Entry.CreatedAt.IsZero() {
		return value, fmt.Errorf("history entry has invalid identity or creation time")
	}
	if request, ok := value.Entry.Payload.(sessionentry.ModelRequest); ok {
		if err := request.Validate(); err != nil {
			return value, err
		}
	}
	return value, nil
}
