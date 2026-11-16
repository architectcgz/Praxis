package session

import (
	"cmp"
	"context"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/identity"
	"slices"
	"time"
)

// AgentView 只携带展示信息，不借用活跃 Run 或展开历史正文。
type AgentView struct {
	ID        identity.AgentID
	SessionID identity.SessionID

	DefinitionID identity.AgentDefinitionID
	Kind         agentmodel.AgentKind
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Name         string                    `json:"name"`
	State        agentmodel.AgentState     `json:"state"`
	CurrentRunID identity.RunID            `json:"currentRunId"`
	CurrentRun   *sessionentry.RunSnapshot `json:"currentRun"`
	LatestRun    *sessionentry.RunSnapshot `json:"latestRun"`
}

func (s *Session) Agents(ctx context.Context, info SessionInfo, definition agentmodel.AgentDefinition, limit int) ([]AgentView, error) {

	runs, err := s.ListSessionRuns(ctx)
	if err != nil {
		return nil, err
	}
	views := []AgentView{{
		ID:        info.MainAgentID(),
		SessionID: info.ID,

		DefinitionID: agentmodel.DefinitionPrimary,
		Kind:         agentmodel.KindMain,
		CreatedAt:    info.CreatedAt,
		UpdatedAt:    info.UpdatedAt,
		Name:         definition.Name,
		State:        agentmodel.AgentIdle,
	}}
	indexes := map[identity.AgentID]int{info.MainAgentID(): 0}
	// 执行记录按新到旧排列，当前执行与最近结束结果分别保留。
	for _, record := range runs {
		index, found := indexes[record.AgentID]
		if !found {
			index = len(views)
			indexes[record.AgentID] = index
			views = append(views, AgentView{
				ID:        record.AgentID,
				SessionID: info.ID,

				DefinitionID: record.DefinitionID,
				Kind:         agentmodel.KindSubagent,
				CreatedAt:    record.CreatedAt,
				Name:         record.AgentName,
			})
		}
		view := &views[index]
		if view.CurrentRun == nil && view.LatestRun == nil {
			view.State = record.AgentState()
			view.UpdatedAt = record.CreatedAt
			if !record.StartedAt.IsZero() {
				view.UpdatedAt = record.StartedAt
			}
			if !record.EndedAt.IsZero() {
				view.UpdatedAt = record.EndedAt
			}
		}
		if record.Active() && view.CurrentRun == nil {
			view.CurrentRunID = record.ID
			view.CurrentRun = new(record.RunSnapshot)
		}
		if record.Status == sessionentry.RunEnded && view.LatestRun == nil {
			view.LatestRun = new(record.RunSnapshot)
		}
	}
	slices.SortFunc(views[1:], func(a, b AgentView) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	if limit > 0 {
		views = views[:min(limit, len(views))]
	}
	return views, nil
}
