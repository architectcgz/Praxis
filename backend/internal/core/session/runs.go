package session

import (
	"cmp"
	"context"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"slices"
	"time"
)

// FindTaskInputID 只定位已接纳任务的输入，不重建执行对象。
func (s *Session) FindTaskInputID(ctx context.Context, taskID identity.TaskID) (string, error) {
	var inputID string
	err := s.walkHistory(ctx, s.data.MainAgentID(), func(value *sessionentry.Entry) bool {
		prepared, ok := value.Payload.(entryPayload.RunPrepared)
		if ok && prepared.SourceTaskID == taskID {
			inputID = prepared.InputEntryID
			return false
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if inputID == "" {
		return "", errorcode.ErrNotFound
	}
	return inputID, nil
}

// AgentView 汇集 Agent 的身份与从 History 恢复的最近执行阶段。
type AgentView struct {
	ID           identity.AgentID
	SessionID    identity.SessionID
	DefinitionID identity.AgentDefinitionID
	Kind         agentmodel.AgentKind
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Name         string
	State        agentmodel.AgentState
	CurrentRun   *sessionentry.RunSnapshot
	LatestRun    *sessionentry.RunSnapshot
}

func (s *Session) Agents(ctx context.Context, info SessionInfo, definition agentmodel.AgentDefinition) ([]AgentView, error) {
	views := make([]AgentView, 0)
	for _, id := range s.agentIDs() {
		view := AgentView{
			ID:        id,
			SessionID: info.ID,
			Kind:      agentmodel.KindSubagent,
			State:     agentmodel.AgentIdle,
		}
		if id == info.MainAgentID() {
			view.Kind = agentmodel.KindMain
			view.DefinitionID = definition.ID
			view.Name = definition.Name
			view.CreatedAt = info.CreatedAt
			view.UpdatedAt = info.UpdatedAt
		}
		if err := s.readAgentView(ctx, &view); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	slices.SortFunc(views, func(a, b AgentView) int {
		if a.Kind != b.Kind {
			if a.Kind == agentmodel.KindMain {
				return -1
			}
			return 1
		}
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return views, nil
}

// readAgentView 按需读取最近执行的展示事实；不在 Session 中缓存第二套执行状态。
func (s *Session) readAgentView(ctx context.Context, view *AgentView) error {
	var latestID string
	prepared := false
	return s.walkHistory(ctx, view.ID, func(value *sessionentry.Entry) bool {
		terminal, ended := value.Payload.(entryPayload.RunTerminal)
		if latestID == "" {
			latestID = value.RunID
			view.UpdatedAt = value.CreatedAt
			if !ended {
				view.CurrentRun = &sessionentry.RunSnapshot{
					ID:     identity.RunID(latestID),
					Status: sessionentry.RunStarting,
				}
				view.State = agentmodel.AgentExecuting
			}
		}
		if ended && view.LatestRun == nil {
			view.LatestRun = new(sessionentry.SnapshotFromTerminal(identity.RunID(value.RunID), terminal))
			if view.CurrentRun == nil {
				view.State = sessionentry.AgentStateFromOutcome(terminal.Outcome())
			}
		}
		if value.RunID == latestID {
			if metadata, ok := value.Payload.(entryPayload.RunPrepared); ok {
				prepared = true
				if view.Kind == agentmodel.KindSubagent {
					view.Name = metadata.AgentName
					view.DefinitionID = metadata.DefinitionID
					view.CreatedAt = value.CreatedAt
				}
			}
			if current := view.CurrentRun; current != nil {
				switch fact := value.Payload.(type) {
				case entryPayload.RunEndingStarted:
					current.Status = sessionentry.RunEnding
				case entryPayload.RunStarted:
					current.StartedAt = fact.At
					if current.Status == sessionentry.RunStarting {
						current.Status = sessionentry.RunRunning
					}
				case entryPayload.TurnStarted:
					current.Iterations++
				}
				if request, _ := value.RequestFact(); request != nil && request.Status != sessionentry.RequestRunning {
					current.OutputBytes += int64(request.OutputBytes)
				}
				for _, block := range value.Content {
					switch block := block.(type) {
					case sessionentry.ToolCallBlock:
						current.ToolCalls++
					case sessionentry.ToolResultBlock:
						current.OutputBytes += int64(len(block.Text))
					}
				}
			}
		}
		return !prepared || view.LatestRun == nil
	})
}
