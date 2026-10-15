package jsonl

import (
	"encoding/json"
	"fmt"
	"testing"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
)

func TestLoadMessagesLimitKeepsLatestAndFullBoundary(t *testing.T) {
	store := &Store{objects: make(map[objectKey]object)}
	data, err := json.Marshal(agentmodel.Agent{ID: "agent", SessionID: "session", DefinitionID: agentmodel.DefinitionPrimary})
	if err != nil {
		t.Fatal(err)
	}
	store.objects[objectKey{"agent", "agent"}] = object{scope: "session", data: data}
	for i := 1; i <= 205; i++ {
		value := messageRecord{Data: sessionmodel.MessageData{Sequence: uint64(i)}}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		store.objects[objectKey{"message", fmt.Sprint(i)}] = object{scope: "session", data: data}
	}
	repositories := store.Repositories()
	for _, limit := range []int{0, 200, 300} {
		stream, err := repositories.Messages.LoadMessages(t.Context(), contracts.SessionID("session"), contracts.AgentID("agent"), limit)
		if err != nil {
			t.Fatal(err)
		}
		first, count := uint64(1), 205
		if limit == 200 {
			first, count = 6, 200
		}
		if len(stream.Messages) != count || stream.Messages[0].Sequence != first || stream.SequenceBoundary != 205 {
			t.Fatalf("limit=%d 返回了错误范围或边界：%+v", limit, stream)
		}
	}
}
