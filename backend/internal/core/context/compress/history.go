package compress

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"slices"
)

const (
	maxToolResultTokens = 2048
	omittedToolResult   = "[Tool result body omitted to fit the context window.]"
)

// summaryBoundary 只选择连续历史前缀，不跨越保护条目或拆开工具调用与回执。
// 未闭合工具链和没有持久身份的投影条目不能进入摘要替代范围。
func summaryBoundary(value appcontext.ModelContext) int {
	boundary := len(value.Entries)
	type callKey struct{ executionID, callID, kind string }
	pending := make(map[callKey]bool)
	chainStart := -1
	for index, entry := range value.Entries {
		if entry.Protected || entry.ID == "" {
			boundary = min(boundary, index)
		}
		for _, block := range entry.Content {
			kind := "tool"
			if block.Kind == sessionentry.BlockMCPCall || block.Kind == sessionentry.BlockMCPResult {
				kind = "mcp"
			}
			key := callKey{entry.ExecutionID, block.CallID, kind}
			switch block.Kind {
			case sessionentry.BlockToolCall, sessionentry.BlockMCPCall:
				if chainStart < 0 {
					chainStart = index
				}
				pending[key] = true
			case sessionentry.BlockToolResult, sessionentry.BlockMCPResult:
				delete(pending, key)
			}
		}
		if chainStart >= 0 && len(pending) == 0 {
			if boundary >= chainStart && boundary <= index {
				boundary = chainStart
			}
			chainStart = -1
		}
	}
	if chainStart >= 0 {
		boundary = min(boundary, chainStart)
	}
	return boundary
}

// compactInteractions 只精简前缀之后的模型投影，不改写原历史或保护条目。
// 工具声明和回执保持原有身份；携带工具调用的协议推理状态必须保留，以便合法重放。
func compactInteractions(value appcontext.ModelContext, boundary, resultLimit int) (appcontext.ModelContext, bool) {
	compacted := value.Clone()
	entries := compacted.Entries[:boundary]
	changed := false
	for _, entry := range compacted.Entries[boundary:] {
		if entry.Protected {
			entries = append(entries, entry)
			continue
		}
		if entry.Role == role.Assistant {
			var blocks []sessionentry.Block
			hasCall := false
			for _, block := range entry.Content {
				switch block.Kind {
				case sessionentry.BlockToolCall, sessionentry.BlockMCPCall:
					hasCall = true
					blocks = append(blocks, block)
				case sessionentry.BlockReasoningState:
					blocks = append(blocks, block)
				}
			}
			if !hasCall {
				changed = true
				continue
			}
			changed = changed || len(blocks) != len(entry.Content)
			entry.Content = blocks
		}
		for index, block := range entry.Content {
			if isToolResult(block) && appcontext.EstimateTokens(block.Text) > max(resultLimit, appcontext.EstimateTokens(omittedToolResult)) {
				entry.Content[index].Text = omittedToolResult
				changed = true
			}
		}
		entries = append(entries, entry)
	}
	compacted.Entries = entries
	return compacted, changed
}

func isToolResult(block sessionentry.Block) bool {
	return block.Kind == sessionentry.BlockToolResult || block.Kind == sessionentry.BlockMCPResult
}

// discardResultBodies 在容量仍不足时优先舍弃最大的结果正文，不删除回执本身。
// value 必须是本次压缩独占的副本；调用标识、工具名称和错误标记保持不变。
func discardResultBodies(value *appcontext.ModelContext, limit int) (bool, error) {
	tokens, err := value.EstimatedTokens()
	if err != nil || tokens < limit {
		return false, err
	}
	type resultBody struct{ entry, block, tokens int }
	var bodies []resultBody
	for index, entry := range value.Entries {
		if entry.Protected {
			continue
		}
		for blockIndex, block := range entry.Content {
			size := appcontext.EstimateTokens(block.Text)
			if isToolResult(block) && size > appcontext.EstimateTokens(omittedToolResult) {
				bodies = append(bodies, resultBody{index, blockIndex, size})
			}
		}
	}
	slices.SortFunc(bodies, func(a, b resultBody) int { return cmp.Compare(b.tokens, a.tokens) })
	changed := false
	for _, body := range bodies {
		value.Entries[body.entry].Content[body.block].Text = omittedToolResult
		changed = true
		tokens, err = value.EstimatedTokens()
		if err != nil || tokens < limit {
			return changed, err
		}
	}
	return changed, nil
}

func summaryEntry(format SummaryFormat, text string) sessionentry.ContextEntry {
	return sessionentry.ContextEntry{
		Role:       role.User,
		AuthorKind: sessionentry.AuthorSystem,
		Content:    []sessionentry.Block{{Kind: sessionentry.BlockText, Text: format.ResultPrefix + text}},
	}
}

// Compress 在输入预算的预警线触发，最多将一段连续旧历史替换为一条摘要。
// 当前交互可以只保留工具调用和回执，过长结果只舍弃正文；原始历史不受影响。
func Compress(ctx context.Context, value appcontext.ModelContext, options Options, requestSummary RequestSummaryFunc) (appcontext.ModelContext, bool, error) {
	if err := ctx.Err(); err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if options.ContextWindow <= 0 || options.MaxOutputTokens <= 0 || options.ReservedTokens < 0 || options.MaxOutputTokens >= options.ContextWindow || options.ReservedTokens >= options.ContextWindow-options.MaxOutputTokens {
		return appcontext.ModelContext{}, false, errors.New("context compression limits are invalid")
	}
	budget := options.ContextWindow - options.MaxOutputTokens - options.ReservedTokens
	trigger := percent(budget, compressionTriggerPercent)
	estimated, err := value.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if estimated < trigger {
		return value, false, nil
	}
	boundary := summaryBoundary(value)
	compacted, changed := compactInteractions(value, boundary, min(maxToolResultTokens, max(1, budget/8)))
	// 没有可摘要前缀时，仍可通过当前交互精简和结果正文舍弃控制容量。
	finishWithoutSummary := func() (appcontext.ModelContext, bool, error) {
		discarded, err := discardResultBodies(&compacted, trigger)
		if err != nil {
			return appcontext.ModelContext{}, false, err
		}
		changed = changed || discarded
		tokens, err := compacted.EstimatedTokens()
		if err != nil {
			return appcontext.ModelContext{}, false, err
		}
		if tokens > budget {
			return appcontext.ModelContext{}, false, &appcontext.ContextWindowExceededError{
				EstimatedInputTokens: tokens + options.ReservedTokens,
				MaxOutputTokens:      options.MaxOutputTokens,
				ContextWindow:        options.ContextWindow,
			}
		}
		return compacted, changed, nil
	}
	compactedTokens, err := compacted.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if compactedTokens < trigger {
		return compacted, changed, nil
	}
	if boundary == 0 {
		return finishWithoutSummary()
	}
	compressed := appcontext.ModelContext{
		SystemPrompt: value.SystemPrompt,
		Entries:      []sessionentry.ContextEntry{summaryEntry(options.SummaryFormat, "")},
	}
	for _, entry := range compacted.Entries[boundary:] {
		compressed.Entries = append(compressed.Entries, entry.Clone())
	}
	limit := min(maxSummaryTokens, options.MaxOutputTokens, max(1, percent(budget, compressionTargetPercent)/4))
	_, err = discardResultBodies(&compressed, trigger-limit)
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	retainedTokens, err := compressed.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	limit = min(limit, trigger-retainedTokens-1)
	if limit < 1 {
		return finishWithoutSummary()
	}
	if requestSummary == nil {
		return appcontext.ModelContext{}, false, errors.New("context summary requester is required")
	}
	encoded, err := json.Marshal(summaryHistory(value.Entries[:boundary]))
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	text, err := summarizeTranscript(ctx, encoded, options.ContextWindow, limit, options.SummaryFormat, requestSummary)
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	compressed.Entries[0] = summaryEntry(options.SummaryFormat, text)
	// 工具回执投影可能按块拆分同一条记录，替代关系仍只记录一次原始 Entry 身份。
	replaced := make(map[string]bool, boundary)
	for _, entry := range value.Entries[:boundary] {
		if !replaced[entry.ID] {
			compressed.Entries[0].Replaces = append(compressed.Entries[0].Replaces, entry.ID)
			replaced[entry.ID] = true
		}
	}
	after, err := compressed.EstimatedTokens()
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if after >= estimated || after >= trigger {
		return appcontext.ModelContext{}, false, errors.New("context summary did not sufficiently reduce input")
	}
	return compressed, true, nil
}
