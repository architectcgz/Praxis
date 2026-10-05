package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	toolmodel "praxis/internal/core/tool_invocation"
	turnmodel "praxis/internal/core/turn"
	workflowmodel "praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/timing"
)

func (s *Store) view(ctx context.Context, fn func(map[objectKey]object) error) error {
	if ctx == nil {
		return errors.New("查询 context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx := s.tx(ctx); tx != nil {
		return fn(tx.objects)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("日志已关闭")
	}
	return fn(s.objects)
}

func decode[T any](collection string, value object) (T, error) {
	var result T
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(value.data, &fields); err != nil {
		return result, err
	}
	switch collection {
	case "session":
		fields["ID"], _ = json.Marshal(value.scope)
	case "agent", "turn", "tool", "queue", "context":
		fields["SessionID"], _ = json.Marshal(value.scope)
	case "timing", "usage":
		fields["sessionId"], _ = json.Marshal(value.scope)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&result)
	return result, err
}
func encode(collection string, value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	delete(fields, "SessionID")
	delete(fields, "sessionId")
	delete(fields, "session_id")
	if collection == "session" {
		delete(fields, "ID")
	}
	return json.Marshal(fields)
}
func get[T any](ctx context.Context, s *Store, collection, id string) (T, error) {
	var result T
	err := s.view(ctx, func(objects map[objectKey]object) error {
		value, ok := objects[objectKey{collection, id}]
		if !ok {
			return contracts.ErrNotFound
		}
		var err error
		result, err = decode[T](collection, value)
		return err
	})
	return result, err
}
func list[T any](ctx context.Context, s *Store, collection string, filter func(T) bool) ([]T, error) {
	result := make([]T, 0)
	err := s.view(ctx, func(objects map[objectKey]object) error {
		for key, value := range objects {
			if key.collection != collection {
				continue
			}
			item, err := decode[T](collection, value)
			if err != nil {
				return err
			}
			if filter == nil || filter(item) {
				result = append(result, item)
			}
		}
		return nil
	})
	return result, err
}
func save(ctx context.Context, s *Store, scope, collection, id, kind string, value any) error {
	if validatable, ok := value.(interface{ Validate() error }); ok {
		if err := validatable.Validate(); err != nil {
			return err
		}
	}
	data, err := encode(collection, value)
	if err != nil {
		return err
	}
	tx := s.tx(ctx)
	if tx == nil {
		return errors.New("变更必须位于事务内")
	}
	key := projectionKey(scope, collection, id)
	if prior, ok := tx.objects[key]; ok {
		if prior.scope != scope {
			return contracts.ErrRequestConflict
		}
		if err := immutable(collection, prior.data, data); err != nil {
			return err
		}
	}
	return s.change(ctx, scope, collection, id, kind, data)
}
func immutable(collection string, before, after json.RawMessage) error {
	var left, right map[string]json.RawMessage
	if err := json.Unmarshal(before, &left); err != nil {
		return err
	}
	if err := json.Unmarshal(after, &right); err != nil {
		return err
	}
	var fields []string
	switch collection {
	case "turn":
		fields = []string{"ID", "AgentID", "RequestID", "WorkItemID", "ParentTurnID", "Reason", "CreatedAt", "Input"}
	case "tool":
		fields = []string{"ID", "TurnID", "AgentID", "ProviderToolCallID", "Name", "NormalizedArguments", "ArgumentsDigest", "CreatedAt"}
	case "queue":
		fields = []string{"ID", "AgentID", "Sequence", "RequestID", "InputMessageID", "CreatedAt"}
	case "agent":
		fields = []string{"ID", "DefinitionID", "Profile", "CreatedAt"}
	case "session":
		fields = []string{"ProjectID", "WorkspaceID", "CreatedAt"}
	case "usage":
		fields = []string{"agentId", "turnId", "step"}
	case "policy", "context", "message":
		if !bytes.Equal(before, after) {
			return contracts.ErrRequestConflict
		}
	}
	for _, field := range fields {
		if !bytes.Equal(left[field], right[field]) {
			return fmt.Errorf("%w: %s.%s 不可变", contracts.ErrRequestConflict, collection, field)
		}
	}
	if collection == "turn" && string(left["Status"]) == `"settled"` && !bytes.Equal(before, after) {
		return contracts.ErrRequestConflict
	}
	if collection == "tool" {
		var status toolmodel.ToolInvocationStatus
		if err := json.Unmarshal(left["Status"], &status); err != nil {
			return err
		}
		if status.Terminal() && !bytes.Equal(before, after) {
			return contracts.ErrRequestConflict
		}
	}
	return nil
}
func validateEvent(scope string, event Event) error {
	if event.Type == "session.deleted" {
		if scope == "" || event.Collection != "session" || event.Key != "" || len(event.Payload) > 0 {
			return errors.New("非法删除事件")
		}
		return nil
	}
	kinds := map[string]string{
		"project":   "project.saved",
		"workspace": "workspace.saved",
		"session":   "session.saved",
		"agent":     "agent.saved",
		"turn":      "turn.saved",
		"tool":      "tool.saved",
		"policy":    "policy.saved",
		"queue":     "queue.saved",
		"wait":      "wait.saved",
		"control":   "control.saved",
		"context":   "context.entry_appended",
		"message":   "message.appended",
		"timing":    "timing.saved",
		"usage":     "usage.saved",
	}
	if kinds[event.Collection] == "" || kinds[event.Collection] != event.Type {
		return errors.New("未知事件类型")
	}
	if (scope == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return errors.New("事件文件归属非法")
	}
	if event.Collection != "session" && (event.Key == "" || strings.TrimSpace(event.Key) != event.Key) {
		return errors.New("对象键非法")
	}
	if event.Collection == "session" && event.Key != "" {
		return errors.New("Session 身份只能来自文件名")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("payload 必须是对象")
	}
	for _, field := range []string{"SessionID", "sessionId", "session_id", "version"} {
		if _, ok := fields[field]; ok {
			return errors.New("payload 重复保存 Session ID 或格式版本")
		}
	}
	if event.Collection == "session" {
		if _, exists := fields["ID"]; exists {
			return errors.New("Session 身份只能来自文件名")
		}
	}
	if event.Collection == "message" {
		var record messageRecord
		if err := json.Unmarshal(event.Payload, &record); err != nil {
			return err
		}
		if record.Data.Sequence != event.Sequence {
			return errors.New("消息序号必须与追加事件一致")
		}
	}
	return nil
}

// validate 校验提交后的完整图，允许同一事务按任意顺序创建有关联的对象。
func (s *Store) validate(objects map[objectKey]object) error {
	unique := make(map[string]bool)
	claim := func(key string) error {
		if unique[key] {
			return fmt.Errorf("%w: %s", contracts.ErrRequestConflict, key)
		}
		unique[key] = true
		return nil
	}
	reference := func(collection, id, scope string) error {
		value, ok := objects[objectKey{collection, id}]
		if !ok || value.scope != scope {
			return fmt.Errorf("引用不存在或归属不符: %s/%s", collection, id)
		}
		return nil
	}
	for key, value := range objects {
		var item interface{ Validate() error }
		var identity string
		var err error
		switch key.collection {
		case "project":
			v, e := decode[projectmodel.Project](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("workspace", v.DefaultWorkspaceID.String(), "")
			}
			if err == nil {
				w, _ := decode[workspacemodel.Workspace]("workspace", objects[objectKey{"workspace", v.DefaultWorkspaceID.String()}])
				if w.ProjectID != v.ID {
					err = errors.New("Project 默认工作区归属不符")
				}
			}
		case "workspace":
			v, e := decode[workspacemodel.Workspace](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("project", v.ProjectID.String(), "")
			}
			if err == nil {
				err = claim("workspace:" + v.ProjectID.String() + ":" + v.Path)
			}
		case "session":
			v, e := decode[sessionmodel.Session](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("workspace", v.WorkspaceID.String(), "")
			}
			if err == nil {
				w, _ := decode[workspacemodel.Workspace]("workspace", objects[objectKey{"workspace", v.WorkspaceID.String()}])
				if w.ProjectID != v.ProjectID {
					err = errors.New("Session 工作区归属不符")
				}
			}
		case "agent":
			v, e := decode[agentmodel.Agent](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("session", value.scope, value.scope)
			}
			if err == nil {
				err = reference("policy", policyKey(v.ID, v.SecurityPolicyRevision), value.scope)
			}
			if err == nil && v.DefinitionID == agentmodel.DefinitionPrimary {
				err = claim("primary:" + value.scope)
			}
			if err == nil && v.CurrentTurnID != "" {
				err = reference("turn", v.CurrentTurnID.String(), value.scope)
				if err == nil {
					t, _ := decode[turnmodel.Turn]("turn", objects[objectKey{"turn", v.CurrentTurnID.String()}])
					if t.AgentID != v.ID || !t.Active() {
						err = errors.New("Agent 活动 Turn 不匹配")
					}
				}
			}
		case "turn":
			v, e := decode[turnmodel.Turn](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("agent", v.AgentID.String(), value.scope)
			}
			if err == nil {
				err = claim("request:" + v.AgentID.String() + ":" + v.RequestID.String())
			}
			if err == nil && v.Active() {
				err = claim("active:" + v.AgentID.String())
				if err == nil {
					a, _ := decode[agentmodel.Agent]("agent", objects[objectKey{"agent", v.AgentID.String()}])
					if a.CurrentTurnID != v.ID {
						err = errors.New("活动 Turn 与 Agent 不一致")
					}
				}
			}
			if err == nil && v.ParentTurnID != "" {
				if v.ParentTurnID == v.ID {
					err = errors.New("Turn 不能引用自身为父回合")
				} else {
					err = turnOwner(objects, v.ParentTurnID, v.AgentID, value.scope)
				}
			}
			if err == nil && v.WorkItemID != "" {
				err = reference("queue", v.WorkItemID.String(), value.scope)
				if err == nil {
					work, decodeErr := decode[workflowmodel.QueuedWork]("queue", objects[objectKey{"queue", v.WorkItemID.String()}])
					err = decodeErr
					if err == nil && (work.TurnID != v.ID || work.AgentID != v.AgentID || work.RequestID != v.RequestID) {
						err = errors.New("Turn 与队列项关联不一致")
					}
				}
			}
		case "tool":
			v, e := decode[toolmodel.ToolInvocation](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = turnOwner(objects, v.TurnID, v.AgentID, value.scope)
			}
			if err == nil {
				err = claim("call:" + v.TurnID.String() + ":" + v.ProviderToolCallID)
			}
		case "queue":
			v, e := decode[workflowmodel.QueuedWork](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("agent", v.AgentID.String(), value.scope)
			}
			if err == nil {
				err = claim(fmt.Sprintf("queue:%s:%d", v.AgentID, v.Sequence))
			}
			if err == nil && v.RequestID != "" {
				err = claim("queued-request:" + v.AgentID.String() + ":" + v.RequestID.String())
			}
			if err == nil && v.TurnID != "" {
				err = turnOwner(objects, v.TurnID, v.AgentID, value.scope)
			}
			if err == nil && v.Status == workflowmodel.QueuedWorkRunning {
				err = claim("queued-running:" + v.AgentID.String())
				if err == nil {
					turn, decodeErr := decode[turnmodel.Turn]("turn", objects[objectKey{"turn", v.TurnID.String()}])
					err = decodeErr
					if err == nil && (!turn.Active() || turn.WorkItemID != v.ID) {
						err = errors.New("运行队列项没有对应的活动 Turn")
					}
				}
			}
		case "policy":
			v, e := decode[policyRecord](key.collection, value)
			err = e
			item = v.Policy
			identity = policyKey(v.AgentID, v.Policy.Revision)
			if err == nil {
				err = reference("agent", v.AgentID.String(), value.scope)
			}
		case "context":
			v, e := decode[contextmodel.SessionContextEntry](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("session", value.scope, value.scope)
			}
			if err == nil && v.SourceTurnID != "" {
				err = reference("turn", v.SourceTurnID.String(), value.scope)
			}
		case "wait":
			v, e := decode[workflowmodel.WaitCondition](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = turnOwner(objects, v.TurnID, v.AgentID, value.scope)
			}
		case "control":
			v, e := decode[workflowmodel.AgentControlCommand](key.collection, value)
			err = e
			item = v
			identity = v.ID.String()
			if err == nil {
				err = reference("agent", v.AgentID.String(), value.scope)
			}
			if err == nil && v.TargetTurnID != "" {
				err = turnOwner(objects, v.TargetTurnID, v.AgentID, value.scope)
			}
		case "message":
			v, e := decode[messageRecord](key.collection, value)
			err = e
			item = v.Data
			identity = messageKey(v.OwnerID, v.Data.ID)
			if err == nil {
				err = reference("session", value.scope, value.scope)
			}
			if err == nil && v.OwnerID != "" {
				err = reference("agent", v.OwnerID, value.scope)
			}
			if err == nil && v.Data.TurnID != "" {
				err = reference("turn", v.Data.TurnID, value.scope)
				if err == nil && v.OwnerID != "" {
					err = turnOwner(objects, contracts.TurnID(v.Data.TurnID), contracts.AgentID(v.OwnerID), value.scope)
				}
			}
			if err == nil {
				err = claim(fmt.Sprintf("message-seq:%s:%d", value.scope, v.Data.Sequence))
			}
		case "usage":
			v, e := decode[agentruntime.ModelUsageRecord](key.collection, value)
			err = e
			item = v
			identity = v.Key()
			if err == nil {
				err = turnOwner(objects, contracts.TurnID(v.TurnID), contracts.AgentID(v.AgentID), value.scope)
			}
		case "timing":
			v, e := decode[timing.Record](key.collection, value)
			err = e
			item = v
			identity = v.ID
			if err == nil {
				err = turnOwner(objects, contracts.TurnID(v.TurnID), contracts.AgentID(v.AgentID), value.scope)
			}
		default:
			return errors.New("未知投影类型")
		}
		if err != nil {
			return fmt.Errorf("%s/%s: %w", key.collection, key.id, err)
		}
		if projectionKey(value.scope, key.collection, identity) != key {
			return fmt.Errorf("%s 对象身份与索引不一致", key.collection)
		}
		if err = item.Validate(); err != nil {
			return fmt.Errorf("%s/%s: %w", key.collection, key.id, err)
		}
	}
	return nil
}
func turnOwner(objects map[objectKey]object, id contracts.TurnID, agent contracts.AgentID, scope string) error {
	object, ok := objects[objectKey{"turn", id.String()}]
	if !ok || object.scope != scope {
		return errors.New("Turn 引用不存在或跨 Session")
	}
	value, err := decode[turnmodel.Turn]("turn", object)
	if err != nil {
		return err
	}
	if value.AgentID != agent {
		return errors.New("Turn 与 Agent 归属不符")
	}
	return nil
}
func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		limit = 100
	}
	return values[:min(len(values), limit)]
}
func same(a, b any) bool { return reflect.DeepEqual(a, b) }
func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
