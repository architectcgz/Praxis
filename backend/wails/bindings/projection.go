package bindings

import (
	"praxis/internal/core/entry"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/timing"
	"praxis/internal/dto"
	"strings"
)

// publicMessages 为查询和事件推送提供相同的展示投影，空集合统一编码为数组。
// 内容块按白名单映射，内部推理续传状态和未知块不会穿过协议边界。
func publicMessages(values []entry.Entry) []dto.HistoryMessage {
	result := make([]dto.HistoryMessage, 0, len(values))
	for _, value := range values {
		if value.IsMetadata() {
			continue
		}
		projected := dto.HistoryMessage{
			ID:        value.ID,
			RunID:     value.RunID,
			TurnID:    value.TurnID,
			CreatedAt: value.CreatedAt,
			Role:      string(value.Role),
			Kind:      dto.MessageChat,
			Content:   make([]dto.MessageBlock, 0, len(value.Content)),
		}
		if value.Response != nil {
			projected.RequestID = value.Response.RequestID.String()
			projected.Error = value.Response.Error
			projected.Timing = publicTiming(value.Response.Timing)
		} else if value.Compression != nil {
			projected.Timing = publicTiming(value.Compression.Timing)
		}
		for _, block := range value.Content {
			switch block := block.(type) {
			case entry.TextBlock:
				projected.Content = append(projected.Content, dto.MessageBlock{
					Kind: "text",
					Text: block.Text,
				})
			case entry.ThinkingBlock:
				projected.Content = append(projected.Content, dto.MessageBlock{
					Kind: "thinking",
					Text: block.Text,
				})
			case entry.ToolCallBlock:
				projected.Content = append(projected.Content, dto.MessageBlock{
					Kind:   "tool_call",
					CallID: block.CallID,
					Name:   string(block.Name),
					Input:  block.Arguments,
				})
			case entry.ToolResultBlock:
				projected.Content = append(projected.Content, dto.MessageBlock{
					Timing:  publicTiming(block.Timing),
					Kind:    "tool_result",
					Text:    block.Text,
					CallID:  block.CallID,
					Name:    string(block.Name),
					IsError: block.IsError,
				})
			}
		}
		switch value.Payload.(type) {
		case entry.SubagentReportAccepted:
			projected.Kind = dto.MessageSubagentReport
		case entry.SubagentInstructionAccepted:
			projected.Kind = dto.MessageInstruction
		}
		if value.IsSummary() {
			projected.Kind = dto.MessageSummary
		}
		result = append(result, projected)
	}
	return result
}

func publicHistoryPage(page sessionmodel.HistoryPage) dto.HistoryPage {
	requests := make([]dto.ModelRequestActivity, 0)
	for _, value := range page.Entries {
		if request, measured := value.RequestFact(); request != nil {
			requests = append(requests, publicRequest(value, *request, measured))
		}
	}
	return dto.HistoryPage{
		Messages:   publicMessages(page.Entries),
		Requests:   requests,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	}
}

func publicRequest(value entry.Entry, request entry.ModelRequestRecord, measured *timing.Record) dto.ModelRequestActivity {
	activity := dto.ModelRequestActivity{
		RequestID:   request.ID.String(),
		RunID:       value.RunID,
		TurnID:      value.TurnID,
		RequestKind: string(request.Kind),
		RetryCount:  request.RetryCount,
		CreatedAt:   value.CreatedAt,
		Timing:      publicTiming(measured),
		HistoryEnd:  value.HistoryEnd,
	}
	if request.Status == entry.RequestRunning {
		return activity
	}
	var partial strings.Builder
	for _, block := range request.Partial {
		if text, ok := block.(entry.TextBlock); ok {
			partial.WriteString(text.Text)
		}
	}
	activity.Result = &dto.ModelRequestResult{
		Status:     string(request.Status),
		Error:      request.Error,
		StopReason: request.StopReason,
		Partial:    partial.String(),
	}
	if value := request.Usage; value != nil && value.Reported() {
		activity.Result.Usage = &dto.ModelUsage{
			InputTokens:              value.InputTokens,
			OutputTokens:             value.OutputTokens,
			CacheReadInputTokens:     value.CacheReadInputTokens,
			CacheCreationInputTokens: value.CacheCreationInputTokens,
		}
	}
	return activity
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

func publicUsageSummary(value sessionmodel.UsageSnapshot) dto.SessionUsageSummary {
	ends := make(map[string]int64, len(value.HistoryEnds))
	for id, end := range value.HistoryEnds {
		ends[id.String()] = end
	}
	return dto.SessionUsageSummary{
		TotalRequests:            value.TotalRequests,
		ReportedRequests:         value.ReportedRequests,
		InputTokens:              value.InputTokens,
		OutputTokens:             value.OutputTokens,
		CacheReadInputTokens:     value.CacheReadInputTokens,
		CacheCreationInputTokens: value.CacheCreationInputTokens,
		InputReports:             value.InputReports,
		OutputReports:            value.OutputReports,
		CacheReadReports:         value.CacheReadReports,
		CacheCreationReports:     value.CacheCreationReports,
		HistoryEnds:              ends,
	}
}

func publicRun(value entry.RunSnapshot) dto.RunSummary {
	run := dto.RunSummary{
		ID:          value.ID.String(),
		Status:      string(value.Status),
		StartedAt:   value.StartedAt,
		Iterations:  value.Iterations,
		ToolCalls:   value.ToolCalls,
		OutputBytes: value.OutputBytes,
	}
	if result := value.Result; result != nil {
		run.Result = &dto.RunResult{
			Outcome:        string(result.Outcome),
			EndedAt:        result.EndedAt,
			FailureCode:    string(result.FailureCode),
			FailureMessage: result.FailureMessage,
		}
	}
	return run
}

func publicAgents(agents []sessionmodel.AgentView) []dto.AgentView {
	result := make([]dto.AgentView, 0, len(agents))
	for _, value := range agents {
		agent := dto.AgentView{
			ID:           value.ID.String(),
			SessionID:    value.SessionID.String(),
			DefinitionID: value.DefinitionID.String(),
			Kind:         string(value.Kind),
			CreatedAt:    value.CreatedAt,
			UpdatedAt:    value.UpdatedAt,
			Name:         value.Name,
			State:        string(value.State),
		}
		if run := value.CurrentRun; run != nil {
			agent.CurrentRunID = run.ID.String()
			agent.CurrentRun = new(publicRun(*run))
		}
		if run := value.LatestRun; run != nil {
			agent.LatestRun = new(publicRun(*run))
		}
		result = append(result, agent)
	}
	return result
}
