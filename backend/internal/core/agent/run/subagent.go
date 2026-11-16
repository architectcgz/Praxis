package run

import (
	"context"
	"errors"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"sync"
)

type Assignment struct {
	Task           string          `json:"task"`
	AllowedTools   []tool.ToolName `json:"allowedTools"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
}

type SubagentHandle struct {
	Result  sessionentry.RunSnapshot
	AgentID identity.AgentID
	Run     *Run
	Cancel  context.CancelCauseFunc
	Done    <-chan struct{}
}
type SubagentStatus struct {
	AgentID identity.AgentID         `json:"agentId"`
	Run     sessionentry.RunSnapshot `json:"run"`
}
type collaboration struct {
	mu           sync.Mutex
	children     map[identity.AgentID]SubagentHandle
	reports      []sessionentry.SubagentResult
	consumed     map[string]bool
	accepted     []string
	wake         chan struct{}
	parent       *Run
	spawn        func(context.Context, Assignment, string) (SubagentStatus, error)
	instructions []string
	accepting    bool
}

func (r *Run) initCollaboration() {
	r.collaboration.children = make(map[identity.AgentID]SubagentHandle)
	r.collaboration.consumed = make(map[string]bool)
	r.collaboration.wake = make(chan struct{}, 1)
	r.collaboration.accepting = true
}
func (r *Run) BindSubagents(spawn func(context.Context, Assignment, string) (SubagentStatus, error)) {
	r.collaboration.spawn = spawn
}
func (r *Run) BindParent(parent *Run) { r.collaboration.parent = parent }
func (r *Run) RegisterSubagent(handle SubagentHandle) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	r.collaboration.children[handle.AgentID] = handle
}
func (r *Run) StartSubagent(ctx context.Context, a Assignment, callID string) (SubagentStatus, error) {
	if !r.Main() || r.collaboration.spawn == nil {
		return SubagentStatus{}, fmt.Errorf("subagent delegation is unavailable")
	}
	if a.Task == "" || a.AllowedTools == nil || a.TimeoutSeconds < 1 || a.TimeoutSeconds > 600 {
		return SubagentStatus{}, fmt.Errorf("task, allowedTools and timeoutSeconds (1-600) are required")
	}
	return r.collaboration.spawn(ctx, a, callID)
}
func (r *Run) SubagentStatuses(ids []identity.AgentID) ([]SubagentStatus, error) {
	r.collaboration.mu.Lock()
	handles := make([]SubagentHandle, 0, len(ids))
	if len(ids) == 0 {
		for _, h := range r.collaboration.children {
			handles = append(handles, h)
		}
	} else {
		for _, id := range ids {
			h, ok := r.collaboration.children[id]
			if !ok {
				r.collaboration.mu.Unlock()
				return nil, fmt.Errorf("subagent %s does not belong to this run", id)
			}
			handles = append(handles, h)
		}
	}
	r.collaboration.mu.Unlock()
	statuses := make([]SubagentStatus, len(handles))
	for i, h := range handles {
		snapshot := h.Result
		if h.Run != nil {
			snapshot = h.Run.Snapshot().Snapshot()
		}
		statuses[i] = SubagentStatus{AgentID: h.AgentID, Run: snapshot}
	}
	return statuses, nil
}
func (r *Run) CancelSubagent(id identity.AgentID) error {
	r.collaboration.mu.Lock()
	handle, ok := r.collaboration.children[id]
	r.collaboration.mu.Unlock()
	if !ok {
		return fmt.Errorf("subagent %s does not belong to this run", id)
	}
	handle.Cancel(ErrInterrupted)
	return nil
}
func (r *Run) SendToSubagent(ctx context.Context, id identity.AgentID, text string) (string, error) {
	r.collaboration.mu.Lock()
	handle, ok := r.collaboration.children[id]
	r.collaboration.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("subagent %s does not belong to this run", id)
	}
	if handle.Run == nil {
		return "", fmt.Errorf("subagent execution has ended")
	}
	return handle.Run.receiveInstruction(ctx, text)
}
func (r *Run) receiveInstruction(ctx context.Context, text string) (string, error) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	if !r.collaboration.accepting {
		return "", fmt.Errorf("subagent execution has ended")
	}
	if text == "" {
		return "", fmt.Errorf("instruction text is required")
	}
	id := r.deps.IDs.New("instruction")
	if _, _, err := r.AppendEntry(ctx, sessionentry.ContextEntry{ID: id, Payload: sessionentry.SubagentInstructionReceived{MessageID: id, Text: text}}); err != nil {
		return "", err
	}
	r.collaboration.instructions = append(r.collaboration.instructions, id)
	return id, nil
}
func (r *Run) finishInstructions(ctx context.Context) (bool, error) {
	r.collaboration.mu.Lock()
	if len(r.collaboration.instructions) == 0 {
		r.collaboration.accepting = false
		r.collaboration.mu.Unlock()
		return false, nil
	}
	r.collaboration.mu.Unlock()
	return r.acceptInstructions(ctx)
}

func (r *Run) acceptInstructions(ctx context.Context) (bool, error) {
	r.collaboration.mu.Lock()
	ids := r.collaboration.instructions
	r.collaboration.instructions = nil
	r.collaboration.mu.Unlock()
	for _, id := range ids {
		if _, _, err := r.AppendEntry(ctx, sessionentry.ContextEntry{ID: "accepted:" + id, Payload: sessionentry.SubagentInstructionAccepted{MessageID: id}}); err != nil {
			return false, err
		}
	}
	if err := r.notifyAccepted(ctx, ids); err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}
func (r *Run) ReportToParent(ctx context.Context, text string) error {
	parent := r.collaboration.parent
	if parent == nil {
		return fmt.Errorf("this run has no parent")
	}
	if text == "" {
		return fmt.Errorf("report text is required")
	}
	record := r.Snapshot()
	result := sessionentry.SubagentResult{
		ReportID:     r.deps.IDs.New("report"),
		AgentID:      record.AgentID,
		RunID:        record.ID,
		ParentRunID:  record.ParentRunID,
		ParentCallID: record.ParentCallID,
		Text:         text,
	}
	if _, _, err := r.AppendEntry(ctx, sessionentry.ContextEntry{ID: "sent:" + result.ReportID, Payload: sessionentry.SubagentReportReceived{Report: result}}); err != nil {
		return err
	}
	err := parent.AcceptSubagentResult(ctx, result)
	if err != nil {
		parent.Cancel(err)
		return errors.Join(storageFailure(err), err)
	}
	return nil
}
func (r *Run) AcceptSubagentResult(ctx context.Context, result sessionentry.SubagentResult) error {
	if _, _, err := r.AppendEntry(ctx, sessionentry.ContextEntry{ID: result.ReportID, Payload: sessionentry.SubagentReportReceived{Report: result}}); err != nil {
		return err
	}
	r.collaboration.mu.Lock()
	r.collaboration.reports = append(r.collaboration.reports, result)
	r.collaboration.mu.Unlock()
	select {
	case r.collaboration.wake <- struct{}{}:
	default:
	}
	return nil
}
func (r *Run) WaitSubagents(ctx context.Context, ids []identity.AgentID, all bool) ([]sessionentry.SubagentReceipt, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one subagent is required")
	}
	wanted := make(map[identity.AgentID]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	for {
		r.collaboration.mu.Lock()
		finished := true
		for _, id := range ids {
			h, ok := r.collaboration.children[id]
			if !ok {
				r.collaboration.mu.Unlock()
				return nil, fmt.Errorf("unknown subagent %s", id)
			}
			select {
			case <-h.Done:
			default:
				finished = false
			}
		}
		var fresh []sessionentry.SubagentResult
		for _, report := range r.collaboration.reports {
			if wanted[report.AgentID] && !r.collaboration.consumed[report.ReportID] {
				fresh = append(fresh, report)
			}
		}
		if finished || !all && len(fresh) > 0 {
			receipts := make([]sessionentry.SubagentReceipt, 0, len(fresh))
			for _, report := range fresh {
				r.collaboration.consumed[report.ReportID] = true
				r.collaboration.accepted = append(r.collaboration.accepted, report.ReportID)
				receipts = append(receipts, sessionentry.SubagentReceipt{
					ReportID: report.ReportID,
					AgentID:  report.AgentID,
					Outcome:  report.Outcome,
					Final:    report.Final,
				})
			}
			r.collaboration.mu.Unlock()
			return receipts, nil
		}
		r.collaboration.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.collaboration.wake:
		}
	}
}
func (r *Run) acceptReports(ctx context.Context) error {
	r.collaboration.mu.Lock()
	ids := r.collaboration.accepted
	r.collaboration.accepted = nil
	r.collaboration.mu.Unlock()
	for _, id := range ids {
		if _, _, err := r.AppendEntry(ctx, sessionentry.ContextEntry{ID: "accepted:" + id, Payload: sessionentry.SubagentReportAccepted{ReportID: id}}); err != nil {
			return err
		}
	}
	return r.notifyAccepted(ctx, ids)
}

// finishChildren 保持父执行位置，直到所有子执行的回调与收尾均完成。
func (r *Run) finishChildren() {
	r.collaboration.mu.Lock()
	r.collaboration.accepting = false
	children := make([]SubagentHandle, 0, len(r.collaboration.children))
	for _, h := range r.collaboration.children {
		children = append(children, h)
	}
	r.collaboration.mu.Unlock()
	for _, h := range children {
		h.Cancel(context.Canceled)
	}
	for _, h := range children {
		<-h.Done
	}
}
func (r *Run) ResultForParent() sessionentry.SubagentResult {
	record := r.Snapshot()
	result := sessionentry.SubagentResult{
		ReportID:     r.deps.IDs.New("report"),
		AgentID:      record.AgentID,
		RunID:        record.ID,
		ParentRunID:  record.ParentRunID,
		ParentCallID: record.ParentCallID,
		Outcome:      record.Outcome,
		FailureCode:  record.FailureCode,
		Final:        true,
	}
	if value, ok := r.Entry(record.Progress.FinalAssistantID); ok {
		result.Text = value.TextContent()
	} else {
		r.mu.RLock()
		defer r.mu.RUnlock()
		for i := len(r.entries) - 1; i >= 0; i-- {
			request, _ := r.entries[i].RequestFact()
			if request == nil {
				continue
			}
			for _, b := range request.Partial {
				if text, ok := b.(sessionentry.TextBlock); ok {
					result.Text += text.Text
				}
			}
			if result.Text != "" {
				result.Partial = true
				break
			}
		}
	}
	return result
}

func (r *Run) WakeSubagentWaiters() {
	select {
	case r.collaboration.wake <- struct{}{}:
	default:
	}
}

func (r *Run) notifyAccepted(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	values := make([]sessionentry.ContextEntry, 0, len(ids))
	for _, id := range ids {
		value, err := r.Session.Entry(ctx, "accepted:"+id)
		if err != nil {
			return storageFailure(err)
		}
		values = append(values, *value)
	}
	r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(""), Payload: coreevent.EntriesAppended{Entries: values}})
	return nil
}

// CompleteSubagent 在交付和收尾完成后释放子执行正文，只保留状态和必要控制身份。
func (r *Run) CompleteSubagent(id identity.AgentID, result sessionentry.RunSnapshot) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	h := r.collaboration.children[id]
	h.Run = nil
	h.Result = result
	r.collaboration.children[id] = h
}
