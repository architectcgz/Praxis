package providers

import "praxis/internal/core/model"

// OpenAIChatStopReason 将 finish_reason 映射为统一结束语义；空值或未知值返回 StopUnknown。
func OpenAIChatStopReason(reason string) model.ModelStopReason {
	switch reason {
	case "stop":
		return model.StopCompleted
	case "tool_calls":
		return model.StopToolCalls
	case "length":
		return model.StopTruncated
	case "content_filter":
		return model.StopFiltered
	default:
		return model.StopUnknown
	}
}

// AnthropicStopReason 将 stop_reason 映射为统一结束语义；拒绝和自定义停止序列不作为自然完成。
func AnthropicStopReason(reason string) model.ModelStopReason {
	switch reason {
	case "end_turn":
		return model.StopCompleted
	case "tool_use":
		return model.StopToolCalls
	case "max_tokens", "model_context_window_exceeded":
		return model.StopTruncated
	case "pause_turn":
		return model.StopPaused
	case "refusal":
		return model.StopRefused
	case "stop_sequence":
		return model.StopSequence
	default:
		return model.StopUnknown
	}
}

// OpenAIResponsesStopReason 结合终态 status、incomplete_details.reason 和输出内容判断结束语义。
// hasToolCalls 表示存在函数调用，refused 表示存在拒绝内容；非已知终态返回 StopUnknown。
func OpenAIResponsesStopReason(status, incompleteReason string, hasToolCalls, refused bool) model.ModelStopReason {
	switch status {
	case "completed":
		if refused {
			return model.StopRefused
		}
		if hasToolCalls {
			return model.StopToolCalls
		}
		return model.StopCompleted
	case "incomplete":
		switch incompleteReason {
		case "max_output_tokens":
			return model.StopTruncated
		case "content_filter":
			return model.StopFiltered
		}
	}
	return model.StopUnknown
}
