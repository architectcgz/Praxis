package bindings

import (
	"praxis/internal/core/entry"
	"praxis/internal/core/session"
	"praxis/internal/core/timing"
	"praxis/wails/dto"
)

func publicRun(r entry.RunSnapshot) dto.RunSummary {
	return dto.RunSummary{
		ID:             r.ID.String(),
		Status:         string(r.Status),
		Outcome:        string(r.Outcome),
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		FailureCode:    string(r.FailureCode),
		FailureMessage: r.FailureMessage,
		Iterations:     r.Iterations,
		ToolCalls:      r.ToolCalls,
		OutputBytes:    r.OutputBytes,
	}
}

func publicAgents(values []session.AgentView) []dto.AgentView {
	result := make([]dto.AgentView, 0, len(values))
	for _, v := range values {
		out := dto.AgentView{
			ID:           v.ID.String(),
			SessionID:    v.SessionID.String(),
			DefinitionID: v.DefinitionID.String(),
			Kind:         string(v.Kind),
			CreatedAt:    v.CreatedAt,
			UpdatedAt:    v.UpdatedAt,
			Name:         v.Name,
			State:        string(v.State),
			CurrentRunID: v.CurrentRunID.String(),
		}
		if v.CurrentRun != nil {
			out.CurrentRun = new(publicRun(*v.CurrentRun))
		}
		if v.LatestRun != nil {
			out.LatestRun = new(publicRun(*v.LatestRun))
		}
		result = append(result, out)
	}
	return result
}

func publicTiming(value *timing.Record) *dto.Timing {
	if value == nil {
		return nil
	}
	return &dto.Timing{
		StartedAt:       value.StartedAt,
		FinishedAt:      value.FinishedAt,
		DurationMS:      value.DurationMS,
		FirstResponseMS: value.FirstResponseMS,
	}
}

// publicEntries 为查询和事件推送提供相同的展示投影，空集合统一编码为数组。
// 内容块按白名单映射，内部推理续传状态和未知块不会穿过协议边界。
func publicEntries(values []entry.ContextEntry) []dto.Entry {
	result := make([]dto.Entry, 0, len(values))
	for _, value := range values {
		if request, measured := value.RequestFact(); request != nil {
			if request.Status == entry.RequestRunning || request.Status == entry.RequestCompleted {
				continue
			}
			partial := make([]dto.EntryBlock, 0)
			for _, block := range request.Partial {
				if text, ok := block.(entry.TextBlock); ok {
					partial = append(partial, dto.EntryBlock{
						Kind: "text",
						Text: text.Text,
					})
				}
			}
			result = append(result, dto.Entry{
				ID:          value.ID,
				RunID:       value.RunID,
				TurnID:      value.TurnID,
				CreatedAt:   value.CreatedAt,
				Kind:        dto.EntryModelRequest,
				RequestID:   request.ID.String(),
				RequestKind: string(request.Kind),
				Status:      string(request.Status),
				RetryCount:  request.RetryCount,
				Error:       request.Error,
				Content:     partial,
				Timing:      publicTiming(measured),
			})
			continue
		}
		if value.IsMetadata() && value.ExecutionResult() == nil {
			continue
		}
		projected := dto.Entry{}
		if value.Response != nil {
			projected.RequestID = value.Response.RequestID.String()
			projected.Error = value.Response.Error
			projected.Timing = publicTiming(value.Response.Timing)
		} else if value.Compression != nil {
			projected.Timing = publicTiming(value.Compression.Timing)
		}
		content := make([]dto.EntryBlock, 0, len(value.Content))
		for _, block := range value.Content {
			switch block := block.(type) {
			case entry.TextBlock:
				content = append(content, dto.EntryBlock{
					Kind: "text",
					Text: block.Text,
				})
			case entry.ThinkingBlock:
				content = append(content, dto.EntryBlock{
					Kind: "thinking",
					Text: block.Text,
				})
			case entry.ToolCallBlock:
				content = append(content, dto.EntryBlock{
					Kind:   "tool_call",
					CallID: block.CallID,
					Name:   string(block.Name),
					Input:  block.Arguments,
				})
			case entry.ToolResultBlock:
				content = append(content, dto.EntryBlock{
					Timing:  publicTiming(block.Timing),
					Kind:    "tool_result",
					Text:    block.Text,
					CallID:  block.CallID,
					Name:    string(block.Name),
					IsError: block.IsError,
				})
			}
		}
		kind := dto.EntryMessage
		switch value.Payload.(type) {
		case entry.SubagentReportAccepted:
			kind = dto.EntrySubagentReport
		case entry.SubagentInstructionAccepted:
			kind = dto.EntryInstruction
		}
		if value.IsSummary() {
			kind = dto.EntrySummary
		} else if value.IsRun() {
			kind = dto.EntryRun
		}
		result = append(result, dto.Entry{
			ID:        value.ID,
			RunID:     value.RunID,
			TurnID:    value.TurnID,
			CreatedAt: value.CreatedAt,
			Role:      string(value.Role),
			Kind:      kind,
			Content:   content,
			RequestID: projected.RequestID,
			Error:     projected.Error,
			Timing:    projected.Timing,
		})
	}
	return result
}
