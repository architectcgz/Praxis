package bindings

import (
	sessionentry "praxis/internal/core/entry"
	"praxis/wails/dto"
)

// publicEntries 为查询和事件推送提供相同的展示投影，空集合统一编码为数组。
// 内容块按白名单映射，内部推理续传状态和未知块不会穿过协议边界。
func publicEntries(values []sessionentry.ContextEntry) []dto.Entry {
	result := make([]dto.Entry, 0, len(values))
	for _, value := range values {
		if request, measured := value.RequestFact(); request != nil {
			if request.Status == sessionentry.RequestRunning || request.Status == sessionentry.RequestCompleted {
				continue
			}
			partial := make([]dto.EntryBlock, 0)
			for _, block := range request.Partial {
				if text, ok := block.(sessionentry.TextBlock); ok {
					partial = append(partial, dto.EntryBlock{Kind: "text", Text: text.Text})
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
			case sessionentry.TextBlock:
				content = append(content, dto.EntryBlock{
					Kind: "text",
					Text: block.Text,
				})
			case sessionentry.ThinkingBlock:
				content = append(content, dto.EntryBlock{
					Kind: "thinking",
					Text: block.Text,
				})
			case sessionentry.ToolCallBlock:
				content = append(content, dto.EntryBlock{
					Kind:   "tool_call",
					CallID: block.CallID,
					Name:   string(block.Name),
					Input:  block.Arguments,
				})
			case sessionentry.ToolResultBlock:
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
		case sessionentry.SubagentReportAccepted:
			kind = dto.EntrySubagentReport
		case sessionentry.SubagentInstructionAccepted:
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
