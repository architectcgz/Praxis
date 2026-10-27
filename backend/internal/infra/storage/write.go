package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/infra/jsonl"
	"time"
)

// appendRecord 在内存锁内检查约束、发布新状态并排队保存；不等待磁盘，也不因持久化退化回滚。
func appendRecord(state *Storage, records []logRecord, scope, collection, id, kind string, object any) error {
	if scope != "" && collection != "session" {
		session, err := getRecord[sessionmodel.Session](records, "session", scope)
		if err != nil {
			return err
		}
		if collection != "usage" && collection != "agent_execution_timing" && session.State != sessionmodel.SessionActive {
			return errorcode.ErrRequestConflict
		}
	}
	// 回执更新只持久化身份和正文，其他字段由已接纳的原条目保留。
	payloadValue := object
	if collection == "entry" && kind == "entry.updated" {
		record := object.(entryRecord)
		payloadValue = toolResultUpdate{
			AgentID: record.AgentID,
			EntryID: record.Data.ID,
			Text:    record.Data.Content[0].Text,
		}
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return err
	}
	if state.sequences[scope] == ^uint64(0) {
		return errors.New("storage sequence exhausted")
	}
	event := jsonl.Event{
		Sequence:   state.sequences[scope] + 1,
		ID:         fmt.Sprintf("event:%d", state.sequences[scope]+1),
		Type:       kind,
		Collection: collection,
		Key:        id,
		CreatedAt:  time.Now().UTC(),
		Payload:    payload,
	}
	// 写入值已在所属输入边界校验；只复制可变数据，不重新编码解码或执行恢复校验。
	value := cloneRecordValue(object)
	if err := validateRelations(state, records, scope, collection, value); err != nil {
		return fmt.Errorf("%s/%s: %w", collection, id, err)
	}
	for _, prior := range records {
		if prior.event.Collection != collection || prior.event.Key != id || collection == "entry" && prior.scope != scope {
			continue
		}
		if prior.scope != scope {
			return errorcode.ErrRequestConflict
		}
		if collection == "entry" && kind == "entry.updated" {
			continue
		}
		before, err := json.Marshal(prior.value)
		if err != nil {
			return err
		}
		if bytes.Equal(before, payload) {
			return nil
		}
		if err := immutable(collection, before, payload); err != nil {
			return err
		}
	}
	state.remember(scope, event, value)
	state.enqueue(persistenceOperation{
		kind:      persistAppend,
		scope:     scope,
		projectID: state.projects[scope],
		event:     event,
	})
	return nil
}
