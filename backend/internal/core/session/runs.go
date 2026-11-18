package session

import (
	"cmp"
	"context"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/dto"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"slices"
)

// FindTaskInputID 只定位已接纳任务的输入，不重建执行对象。
func (s *Session) FindTaskInputID(ctx context.Context, taskID identity.TaskID) (string, error) {
	var inputID string
	err := s.walkHistory(ctx, s.data.MainAgentID(), func(value *sessionentry.Entry) bool {
		prepared, ok := value.Payload.(sessionentry.RunPrepared)
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

func (s *Session) Agents(ctx context.Context, info SessionInfo, definition agentmodel.AgentDefinition, limit int) ([]dto.AgentView, error) {
	views := make([]dto.AgentView, 0)
	for _, id := range s.agentIDs() {
		view := dto.AgentView{
			ID:        id.String(),
			SessionID: info.ID.String(),
			Kind:      string(agentmodel.KindSubagent),
			State:     string(agentmodel.AgentIdle),
		}
		if id == info.MainAgentID() {
			view.Kind = string(agentmodel.KindMain)
			view.DefinitionID = definition.ID.String()
			view.Name = definition.Name
			view.CreatedAt = info.CreatedAt
			view.UpdatedAt = info.UpdatedAt
		}
		if err := s.readAgentView(ctx, &view); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	slices.SortFunc(views, func(a, b dto.AgentView) int {
		if a.Kind != b.Kind {
			if a.Kind == string(agentmodel.KindMain) {
				return -1
			}
			return 1
		}
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return capped(views, limit), nil
}

// readAgentView 按需读取最近执行的展示事实；不在 Session 中缓存第二套执行状态。
func (s *Session) readAgentView(ctx context.Context, view *dto.AgentView) error {
	var latestID string
	prepared := false
	return s.walkHistory(ctx, identity.AgentID(view.ID), func(value *sessionentry.Entry) bool {
		if latestID == "" {
			latestID = value.RunID
			view.UpdatedAt = value.CreatedAt
			if !value.IsRun() {
				view.CurrentRunID = latestID
				view.CurrentRun = &dto.RunSummary{
					ID:     view.CurrentRunID,
					Status: string(sessionentry.RunStarting),
				}
				view.State = string(agentmodel.AgentExecuting)
			}
		}
		if result := value.ExecutionResult(); result != nil && view.LatestRun == nil {
			view.LatestRun = new(result.Snapshot(identity.RunID(value.RunID)))
			if view.CurrentRun == nil {
				view.State = string(result.AgentState())
			}
		}
		if value.RunID == latestID {
			if metadata, ok := value.Payload.(sessionentry.RunPrepared); ok {
				prepared = true
				if view.Kind == string(agentmodel.KindSubagent) {
					view.Name = metadata.AgentName
					view.DefinitionID = metadata.DefinitionID.String()
					view.CreatedAt = value.CreatedAt
				}
			}
			if current := view.CurrentRun; current != nil {
				switch fact := value.Payload.(type) {
				case sessionentry.RunEndingStarted:
					current.Status = string(sessionentry.RunEnding)
				case sessionentry.RunStarted:
					current.StartedAt = fact.At
					if current.Status == string(sessionentry.RunStarting) {
						current.Status = string(sessionentry.RunRunning)
					}
				case sessionentry.TurnStarted:
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
