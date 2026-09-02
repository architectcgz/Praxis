package orchestrate

import (
	"context"
	"encoding/json"
	"errors"
	domaincontext "praxis/internal/core/domain/context"
	"strconv"
	"strings"

	domaincommand "praxis/internal/core/domain/command"
	domainfoundation "praxis/internal/core/domain/foundation"
)

func fmtUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}

type AppendSessionContextRequest struct {
	RequestID         domainfoundation.RequestID
	SessionID         domainfoundation.SessionID
	ExpectedRevision  uint64
	Kind              domaincontext.SessionContextKind
	SourceExecutionID domainfoundation.AgentExecutionID
	Content           string
}

type AppendSessionContextResult struct {
	Entry           domaincontext.SessionContextEntry
	ExistingRequest bool
}

func (o *AgentOrchestrator) AppendSessionContext(ctx context.Context, request AppendSessionContextRequest) (AppendSessionContextResult, error) {
	if ctx == nil {
		return AppendSessionContextResult{}, errors.New("append session context is required")
	}
	if !o.Ready() || request.RequestID == "" || request.SessionID == "" || strings.TrimSpace(request.Content) == "" {
		return AppendSessionContextResult{}, commandError(CommandErrorInvalidRequest)
	}
	digest := commandArgumentsDigest(struct {
		SessionID domainfoundation.SessionID
		Revision  uint64
		Kind      domaincontext.SessionContextKind
		Source    domainfoundation.AgentExecutionID
		Content   string
	}{request.SessionID, request.ExpectedRevision, request.Kind, request.SourceExecutionID, strings.TrimSpace(request.Content)})
	var result AppendSessionContextResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		if receipt, found, err := commandReceipt(txCtx, o.commandReceipts, request.RequestID, "append_session_context", digest); err != nil {
			return err
		} else if found {
			var value struct {
				SessionID string
				Revision  uint64
			}
			if err := json.Unmarshal(receipt.ResultPayload, &value); err != nil {
				return err
			}
			entries, err := o.contexts.List(txCtx, domainfoundation.SessionID(value.SessionID), value.Revision-1, 1)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return domainfoundation.ErrNotFound
			}
			result = AppendSessionContextResult{Entry: entries[0], ExistingRequest: true}
			return nil
		}
		if o.contexts == nil {
			return errors.New("session context repository is unavailable")
		}
		current, err := o.contexts.CurrentRevision(txCtx, request.SessionID)
		if err != nil {
			return err
		}
		if current != request.ExpectedRevision {
			return domainfoundation.ErrRevisionConflict
		}
		entry, err := domaincontext.NewSessionContextEntry(request.SessionID, current+1, request.Kind, request.SourceExecutionID, strings.TrimSpace(request.Content), o.clock.Now())
		if err != nil {
			return err
		}
		if err := o.contexts.Append(txCtx, entry, current); err != nil {
			return err
		}
		event := o.newEvent(domainfoundation.EventSessionContextAppended, entry.CreatedAt)
		event.SessionID = entry.SessionID
		event.Payload = map[string]string{"revision": strconv.FormatUint(entry.Revision, 10), "kind": string(entry.Kind)}
		if err := o.appendEvent(txCtx, event); err != nil {
			return err
		}
		if o.commandReceipts != nil {
			payload, _ := json.Marshal(struct {
				SessionID string
				Revision  uint64
			}{entry.SessionID.String(), entry.Revision})
			if err := o.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{RequestID: request.RequestID, Command: "append_session_context", ArgumentsDigest: digest, ResultPayload: payload, CreatedAt: entry.CreatedAt}); err != nil {
				return err
			}
		}
		result.Entry = entry
		return nil
	})
	return result, err
}
