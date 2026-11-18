package bindings

import (
	"praxis/internal/core/entry"
	"praxis/internal/dto"
)

// publicEntries 为查询和事件推送提供相同的展示投影，空集合统一编码为数组。
// 内容块按白名单映射，内部推理续传状态和未知块不会穿过协议边界。
func publicEntries(values []entry.Entry) []dto.Entry {
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
				Timing:      measured,
			})
			continue
		}
		if value.IsMetadata() && value.ExecutionResult() == nil {
			continue
		}
		projected := dto.Entry{
			ID:        value.ID,
			RunID:     value.RunID,
			TurnID:    value.TurnID,
			CreatedAt: value.CreatedAt,
			Role:      string(value.Role),
			Kind:      dto.EntryMessage,
			Content:   make([]dto.EntryBlock, 0, len(value.Content)),
		}
		if value.Response != nil {
			projected.RequestID = value.Response.RequestID.String()
			projected.Error = value.Response.Error
			projected.Timing = value.Response.Timing
		} else if value.Compression != nil {
			projected.Timing = value.Compression.Timing
		}
		for _, block := range value.Content {
			switch block := block.(type) {
			case entry.TextBlock:
				projected.Content = append(projected.Content, dto.EntryBlock{
					Kind: "text",
					Text: block.Text,
				})
			case entry.ThinkingBlock:
				projected.Content = append(projected.Content, dto.EntryBlock{
					Kind: "thinking",
					Text: block.Text,
				})
			case entry.ToolCallBlock:
				projected.Content = append(projected.Content, dto.EntryBlock{
					Kind:   "tool_call",
					CallID: block.CallID,
					Name:   string(block.Name),
					Input:  block.Arguments,
				})
			case entry.ToolResultBlock:
				projected.Content = append(projected.Content, dto.EntryBlock{
					Timing:  block.Timing,
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
			projected.Kind = dto.EntrySubagentReport
		case entry.SubagentInstructionAccepted:
			projected.Kind = dto.EntryInstruction
		}
		if value.IsSummary() {
			projected.Kind = dto.EntrySummary
		} else if value.IsRun() {
			projected.Kind = dto.EntryRun
		}
		result = append(result, projected)
	}
	return result
}
