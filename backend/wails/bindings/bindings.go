package bindings

import (
	"context"
	"fmt"
	"praxis/internal/entry"
	coreevent "praxis/internal/event"
	sessionservice "praxis/internal/service/session"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type Host interface {
	BindingContext() (context.Context, Services, error)
	EmitEvent(string, any)
	SelectDirectory(string) (string, error)
	LogError(string, ...any)
}

type Bindings struct {
	host     Host
	projects *ProjectBindings
	sessions *SessionBindings
	agents   *AgentBindings
	commands *CommandBindings
}

type PageConfig struct {
	DefaultHistorySize int
	MaxHistorySize     int
	ListLimit          int
}

func New(host Host, pages PageConfig) *Bindings {
	return &Bindings{
		host: host,
		projects: &ProjectBindings{
			host:      host,
			listLimit: pages.ListLimit,
		},
		sessions: &SessionBindings{
			host:               host,
			defaultHistorySize: pages.DefaultHistorySize,
			listLimit:          pages.ListLimit,
		},
		agents: &AgentBindings{
			host:               host,
			defaultHistorySize: pages.DefaultHistorySize,
			historyLimitRule:   fmt.Sprintf("min=1,max=%d", pages.MaxHistorySize),
		},
		commands: &CommandBindings{host: host},
	}
}

func (b *Bindings) All() []application.Service {
	return []application.Service{
		application.NewService(b.projects),
		application.NewService(b.sessions),
		application.NewService(b.agents),
		application.NewService(b.commands),
	}
}

// EmitAgentEvent 显式映射核心载荷和条目，不向前端序列化领域对象。
func (b *Bindings) EmitAgentEvent(event coreevent.Event) {
	runID, turnID := event.Source.RunID.String(), event.Source.TurnID.String()
	var data any
	var requestInfo *coreevent.ModelRequestInfo
	var requestResult *ModelRequestResult
	switch payload := event.Payload.(type) {
	case coreevent.SessionConfigReloaded:
		data = AgentSessionConfigReloaded{
			Models:         publicModelOptions(payload.Models),
			ModelSelection: ModelSelection(payload.ModelChoice),
			ApprovalMode: publicApprovalMode(sessionservice.ApprovalSettings{
				SessionID:     event.Source.SessionID,
				GlobalMode:    payload.GlobalMode,
				SessionMode:   payload.SessionMode,
				EffectiveMode: payload.EffectiveMode,
			}),
		}
	case coreevent.SessionTitleChanged:
		data = AgentSessionTitleChanged{
			Title:     payload.Title,
			UpdatedAt: payload.UpdatedAt,
		}
	case coreevent.AgentStateChanged:
		data = AgentStateChanged{State: string(payload.State)}
	case coreevent.EntriesAppended:
		messages := publicMessages(payload.Entries)
		if len(messages) == 0 {
			return
		}
		data = AgentMessagesAppended{Messages: messages}
	case coreevent.TurnStarted, coreevent.TurnCompleted, coreevent.TurnFailed, coreevent.TurnInterrupted, coreevent.RunEnding, coreevent.SubagentReleased:
		data = struct{}{}
	case coreevent.ModelRequestStarted:
		requestInfo = &payload.Info
	case coreevent.ModelRequestCompleted:
		requestInfo = &payload.Info
		requestResult = &ModelRequestResult{
			Status:     string(entry.RequestCompleted),
			StopReason: payload.StopReason,
			Usage:      (*ModelUsage)(payload.Usage),
		}
	case coreevent.ModelRequestFailed:
		requestInfo = &payload.Info
		requestResult = &ModelRequestResult{
			Status:     string(entry.RequestFailed),
			StopReason: payload.StopReason,
			Error:      payload.Error,
			Partial:    payload.Partial,
			Usage:      (*ModelUsage)(payload.Usage),
		}
	case coreevent.ModelRequestInterrupted:
		requestInfo = &payload.Info
		requestResult = &ModelRequestResult{
			Status:     string(entry.RequestInterrupted),
			StopReason: payload.StopReason,
			Error:      payload.Error,
			Partial:    payload.Partial,
			Usage:      (*ModelUsage)(payload.Usage),
		}
	case coreevent.RunStarted:
		data = AgentRunStarted{StartedAt: payload.StartedAt}
	case coreevent.SubagentStarted:
		data = AgentSubagentStarted{
			Name:          payload.Name,
			CreatedAt:     payload.CreatedAt,
			Messages:      publicMessages(payload.Entries),
			ParentAgentID: payload.ParentAgentID.String(),
			ParentRunID:   payload.ParentRunID.String(),
			CallID:        payload.CallID,
		}
	case coreevent.TaskQueueChanged:
		tasks := make([]QueuedTask, 0, len(payload.Tasks))
		for _, task := range payload.Tasks {
			tasks = append(tasks, QueuedTask{
				ID:        task.ID.String(),
				Prompt:    task.Prompt,
				Sequence:  task.Sequence,
				CreatedAt: task.CreatedAt,
			})
		}
		data = AgentTaskQueueChanged{Tasks: tasks, Paused: payload.Paused}
	case coreevent.TextDelta:
		data = AgentTextDelta{Text: payload.Text}
	case coreevent.ThinkingDelta:
		data = AgentThinkingDelta{Text: payload.Text}
	case coreevent.ToolCall:
		data = AgentToolCall{
			CallID:    payload.CallID,
			Name:      payload.Name,
			Arguments: payload.Arguments,
		}
	case coreevent.ToolApprovalRequired:
		data = ToolApproval{
			ToolDecisionRequest: ToolDecisionRequest{
				SessionID: event.Source.SessionID.String(),
				AgentID:   event.Source.AgentID.String(),
				RunID:     runID,
				CallID:    payload.CallID,
			},
			Name:        payload.Name,
			RuleID:      payload.RuleID,
			Arguments:   payload.Arguments,
			Action:      string(payload.Action),
			RiskVerdict: string(payload.RiskVerdict),
			RiskReason:  payload.RiskReason,
		}
	case coreevent.ToolApprovalResolved:
		data = AgentToolApprovalResolved{CallID: payload.CallID}
	case coreevent.ToolStarted:
		data = AgentToolStarted{
			CallID: payload.CallID,
			Timing: (*Timing)(payload.Timing),
		}
	case coreevent.ToolResult:
		data = AgentToolResult{
			CallID:  payload.CallID,
			Result:  payload.Result,
			IsError: payload.IsError,
			Timing:  (*Timing)(payload.Timing),
		}
	case coreevent.RunEnded:
		data = AgentRunEnded{
			StartedAt:      payload.StartedAt,
			EndedAt:        payload.EndedAt,
			State:          string(payload.State),
			Outcome:        string(payload.Outcome),
			Iterations:     payload.Iterations,
			ToolCalls:      payload.ToolCalls,
			FailureCode:    string(payload.FailureCode),
			FailureMessage: payload.FailureMessage,
		}
	case coreevent.ErrorOccurred:
		data = AgentErrorOccurred{Message: payload.Message}
	case coreevent.TaskQueueStorageFailed:
		data = AgentErrorOccurred{Message: payload.Message}
	default:
		b.host.LogError("unsupported agent event payload: %T", event.Payload)
		return
	}
	if requestInfo != nil {
		data = ModelRequestActivity{
			RequestID:   requestInfo.RequestID.String(),
			RunID:       runID,
			TurnID:      turnID,
			RequestKind: string(requestInfo.Kind),
			RetryCount:  requestInfo.RetryCount,
			CreatedAt:   requestInfo.CreatedAt,
			Timing:      (*Timing)(requestInfo.Timing),
			HistoryEnd:  requestInfo.HistoryEnd,
			Result:      requestResult,
		}
	}
	b.host.EmitEvent("praxis:session:"+event.Source.SessionID.String(), AgentEvent{
		Kind:    string(event.Kind()),
		AgentID: event.Source.AgentID.String(),
		RunID:   runID,
		TurnID:  turnID,
		Data:    data,
	})
}
