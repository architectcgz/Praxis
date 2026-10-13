package jsonl

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	toolmodel "praxis/internal/core/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"
)

type receiptKey struct{ scope, owner, task, call string }
type pendingReceipt struct {
	requestID string
	name      string
	sequence  uint64
	index     int
}

// indexToolReceipts 随事务及日志重放更新缺口索引，普通读取无需扫描完整对话。
func indexToolReceipts(pending map[receiptKey]pendingReceipt, receipts map[receiptKey]bool, scope string, record messageRecord) {
	callIndex := 0
	for _, block := range record.Data.Blocks {
		key := receiptKey{
			scope,
			record.OwnerID,
			record.Data.TaskID,
			block.CallID,
		}
		switch block.Kind {
		case sessionmodel.BlockToolCall:
			if !receipts[key] {
				pending[key] = pendingReceipt{
					requestID: record.Data.RequestID,
					name:      block.Name,
					sequence:  record.Data.Sequence,
					index:     callIndex,
				}
			}
			callIndex++
		case sessionmodel.BlockToolResult:
			receipts[key] = true
			delete(pending, key)
		}
	}
}

// ensureToolReceipts 仅追加确认停止的调用事实；活跃调用和人工审批保持可恢复。
func (r MessageStreams) ensureToolReceipts(ctx context.Context, owner agentmodel.Agent) error {
	tx := r.s.tx(ctx)
	keys := make([]receiptKey, 0)
	for key := range tx.pending {
		if key.scope == owner.SessionID.String() && key.owner == appendOwner(owner) {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b receiptKey) int {
		return cmp.Or(cmp.Compare(tx.pending[a].sequence, tx.pending[b].sequence), cmp.Compare(tx.pending[a].index, tx.pending[b].index))
	})
	for _, key := range keys {
		missing := tx.pending[key]
		invocation, err := (ToolRepository{r.s}).FindByTaskCall(ctx, contracts.TaskID(key.task), key.call)
		if err != nil {
			return fmt.Errorf("tool call %s has no valid invocation: %w", key.call, err)
		}
		task, err := (TaskRepository{r.s}).Get(ctx, invocation.TaskID)
		if err != nil {
			return err
		}
		at := time.Now().UTC()
		switch invocation.Status {
		case toolmodel.ToolInvocationAwaitingApproval:
			if task.Active() {
				continue
			}
			fallthrough
		case toolmodel.ToolInvocationRequested, toolmodel.ToolInvocationApproved:
			if task.Active() {
				continue
			}
			result := toolcontracts.NewToolError(string(toolmodel.ToolFailureInterrupted), "The tool call was interrupted before execution.")
			err = invocation.Interrupt(toolmodel.ToolInvocationResult{InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureInterrupted}, at)
		case toolmodel.ToolInvocationRunning:
			if task.Active() && !r.s.staleRunning[invocation.ID.String()] {
				continue
			}
			result := toolcontracts.NewToolError(string(toolmodel.ToolFailureResultUnknown), "Execution stopped after the tool started. Its side effects are unknown; do not automatically retry.")
			err = invocation.MarkResultUnknown(toolmodel.ToolInvocationResult{InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureResultUnknown}, at)
		}
		if err != nil {
			return err
		}
		if !invocation.Status.Terminal() {
			continue
		}
		if err := (ToolRepository{r.s}).Save(ctx, invocation); err != nil {
			return err
		}
		payload := invocation.Result.InlineContent
		if payload == "" {
			payload = "(empty tool result)"
		}
		_, err = r.Append(ctx, owner, sessionmodel.MessageData{
			ID:         "tool-result:" + key.task + ":" + key.call,
			RequestID:  missing.requestID,
			TaskID:     key.task,
			TurnID:     invocation.TurnID.String(),
			Role:       sessionmodel.RoleTool,
			AuthorKind: sessionmodel.AuthorTool,
			AuthorID:   missing.name,
			Blocks: []sessionmodel.Block{{
				Kind:    sessionmodel.BlockToolResult,
				Text:    payload,
				CallID:  key.call,
				Name:    missing.name,
				IsError: invocation.Result.ErrorCode != "",
			}},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func clearReceiptScope(pending map[receiptKey]pendingReceipt, receipts map[receiptKey]bool, scope string) {
	for key := range pending {
		if key.scope == scope {
			delete(pending, key)
		}
	}
	for key := range receipts {
		if key.scope == scope {
			delete(receipts, key)
		}
	}
}
