package run

import (
	"context"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/identity"
	"sync"
)

type SubagentHandle struct {
	AgentID identity.AgentID
	Run     *Run
	Cancel  context.CancelCauseFunc
	Done    <-chan struct{}
}

// SubagentReport 是子 Run 交给父 Run 的运行时报告。
type SubagentReport struct {
	ReportID     identity.ReportID
	AgentID      identity.AgentID
	RunID        identity.RunID
	ParentRunID  identity.RunID
	ParentCallID string
	Outcome      sessionentry.RunOutcome
	FailureCode  sessionentry.RunFailureCode
	Text         string
	Final        bool
	Partial      bool
}

type instruction struct {
	id   identity.EntryID
	text string
}

type collaboration struct {
	mu           sync.Mutex
	children     map[identity.AgentID]SubagentHandle
	reports      []SubagentReport
	consumed     map[identity.ReportID]bool
	accepted     []SubagentReport
	wake         chan struct{}
	parent       *Run
	instructions []instruction
	accepting    bool
}

func (r *Run) initCollaboration() {
	r.collaboration.children = make(map[identity.AgentID]SubagentHandle)
	r.collaboration.consumed = make(map[identity.ReportID]bool)
	r.collaboration.wake = make(chan struct{})
	r.collaboration.accepting = true
}

func (r *Run) BindParent(parent *Run) { r.collaboration.parent = parent }

// ReserveSubagent 在协作锁内原子检查活跃名额并占位；未启动前必须通过 ReleaseSubagent 释放。
func (r *Run) ReserveSubagent(id identity.AgentID, limit int) bool {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	if !r.collaboration.accepting {
		return false
	}
	active := 0
	for _, child := range r.collaboration.children {
		if child.Done == nil {
			active++
			continue
		}
		select {
		case <-child.Done:
		default:
			active++
		}
	}
	if active >= limit {
		return false
	}
	r.collaboration.children[id] = SubagentHandle{AgentID: id}
	return true
}

// AttachSubagent 为已预留的子代理名额绑定运行句柄。
func (r *Run) AttachSubagent(handle SubagentHandle) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	r.collaboration.children[handle.AgentID] = handle
}

// ReleaseSubagent 移除尚未启动的预留或失败子代理占位。
func (r *Run) ReleaseSubagent(id identity.AgentID) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	delete(r.collaboration.children, id)
}

func (r *Run) CancelSubagent(id identity.AgentID) error {
	r.collaboration.mu.Lock()
	handle, ok := r.collaboration.children[id]
	r.collaboration.mu.Unlock()
	if !ok {
		return fmt.Errorf("subagent %s does not belong to this run", id)
	}
	if handle.Cancel == nil {
		return fmt.Errorf("subagent %s is not running", id)
	}
	handle.Cancel(ErrInterrupted)
	return nil
}

// CancelSubagentRun 将停止信号交给本 Run 持有的指定子执行。
func (r *Run) CancelSubagentRun(id identity.RunID) {
	r.collaboration.mu.Lock()
	for _, child := range r.collaboration.children {
		if child.Run != nil && child.Run.ID() == id {
			r.collaboration.mu.Unlock()
			if !child.Run.TerminalSaved() {
				child.Cancel(ErrInterrupted)
			}
			return
		}
	}
	r.collaboration.mu.Unlock()
}

func (r *Run) SendToSubagent(ctx context.Context, id identity.AgentID, text string) (identity.EntryID, error) {
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

// receiveInstruction 只把指令交给 Subagent 的运行时协作队列，不直接写 History。
func (r *Run) receiveInstruction(ctx context.Context, text string) (identity.EntryID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.collaboration.mu.Lock()
	if !r.collaboration.accepting {
		r.collaboration.mu.Unlock()
		return "", fmt.Errorf("subagent execution has ended")
	}
	id := identity.NewEntryID()
	r.collaboration.instructions = append(r.collaboration.instructions, instruction{id: id, text: text})
	r.collaboration.mu.Unlock()
	return id, nil
}

func (r *Run) finishInstructions(ctx context.Context) (bool, error) {
	r.collaboration.mu.Lock()
	if len(r.collaboration.instructions) == 0 {
		r.collaboration.accepting = false
		r.collaboration.mu.Unlock()
		return false, nil
	}
	instructions := r.collaboration.instructions
	r.collaboration.instructions = nil
	r.collaboration.mu.Unlock()
	return r.appendInstructions(ctx, instructions)
}

// acceptInstructions 在 Subagent Loop 的串行点把运行时指令变成普通 Input Entry。
func (r *Run) acceptInstructions(ctx context.Context) (bool, error) {
	r.collaboration.mu.Lock()
	instructions := r.collaboration.instructions
	r.collaboration.instructions = nil
	r.collaboration.mu.Unlock()
	return r.appendInstructions(ctx, instructions)
}

func (r *Run) appendInstructions(ctx context.Context, instructions []instruction) (bool, error) {
	if len(instructions) == 0 {
		return false, nil
	}
	for _, instruction := range instructions {
		stored, _, err := r.AppendEntry(ctx, sessionentry.Entry{
			ID:         instruction.id,
			AuthorKind: sessionentry.AuthorAgent,
			Payload: sessionentry.Message{
				Role:    sessionentry.RoleUser,
				Content: []sessionentry.ContentBlock{sessionentry.TextBlock{Text: instruction.text}},
			},
		})
		if err != nil {
			return false, err
		}
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(""),
			Payload: coreevent.EntriesAppended{Entries: []*sessionentry.Entry{stored}},
		})
	}
	return true, nil
}

// ReportToParent 只把报告投递到 Parent Run Memory，由 Parent Loop 负责持久化。
func (r *Run) ReportToParent(ctx context.Context, text string) error {
	parent := r.collaboration.parent
	if parent == nil {
		return fmt.Errorf("this run has no parent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result := SubagentReport{
		ReportID:     identity.NewReportID(),
		AgentID:      r.AgentID(),
		RunID:        r.id,
		ParentRunID:  parent.ID(),
		ParentCallID: r.parentCallID,
		Text:         text,
	}
	return parent.AcceptSubagentResult(ctx, result)
}

// AcceptSubagentResult 只更新 Parent Run Memory，不绕过 Parent Loop 写入 History。
func (r *Run) AcceptSubagentResult(ctx context.Context, result SubagentReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.collaboration.mu.Lock()
	r.collaboration.reports = append(r.collaboration.reports, result)
	r.wakeLocked()
	r.collaboration.mu.Unlock()
	return nil
}

// SubagentReceipt 不包含报告正文；父 Run 会将正文作为独立条目写入历史。
type SubagentReceipt struct {
	ReportID identity.ReportID       `json:"reportId"`
	AgentID  identity.AgentID        `json:"agentId"`
	Outcome  sessionentry.RunOutcome `json:"outcome"`
	Final    bool                    `json:"final"`
}

func (r *Run) WaitSubagents(ctx context.Context, ids []identity.AgentID, all bool) ([]SubagentReceipt, error) {
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
			if h.Done == nil {
				finished = false
				continue
			}
			select {
			case <-h.Done:
			default:
				finished = false
			}
		}
		var fresh []SubagentReport
		for _, report := range r.collaboration.reports {
			if wanted[report.AgentID] && !r.collaboration.consumed[report.ReportID] {
				fresh = append(fresh, report)
			}
		}
		if finished || !all && len(fresh) > 0 {
			receipts := make([]SubagentReceipt, 0, len(fresh))
			for _, report := range fresh {
				r.collaboration.consumed[report.ReportID] = true
				r.collaboration.accepted = append(r.collaboration.accepted, report)
				receipts = append(receipts, SubagentReceipt{
					ReportID: report.ReportID,
					AgentID:  report.AgentID,
					Outcome:  report.Outcome,
					Final:    report.Final,
				})
			}
			r.collaboration.mu.Unlock()
			return receipts, nil
		}
		wake := r.collaboration.wake
		r.collaboration.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wake:
		}
	}
}

// acceptReports 在 Parent Loop 的串行点把自包含报告变成普通 Input Entry。
func (r *Run) acceptReports(ctx context.Context) error {
	r.collaboration.mu.Lock()
	reports := r.collaboration.accepted
	r.collaboration.accepted = nil
	r.collaboration.mu.Unlock()
	for _, report := range reports {
		text := subagentReportText(report)
		stored, _, err := r.AppendEntry(ctx, sessionentry.Entry{
			ID:         identity.NewEntryID(),
			AuthorKind: sessionentry.AuthorAgent,
			Payload: sessionentry.Message{
				Role:    sessionentry.RoleUser,
				Content: []sessionentry.ContentBlock{sessionentry.TextBlock{Text: text}},
			},
		})
		if err != nil {
			return err
		}
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(""),
			Payload: coreevent.EntriesAppended{Entries: []*sessionentry.Entry{stored}},
		})
	}
	return nil
}

func subagentReportText(report SubagentReport) string {
	status := string(report.Outcome)
	if status == "" {
		status = "progress"
	}
	return fmt.Sprintf("[Subagent report %s] agent=%s run=%s outcome=%s final=%t partial=%t failure=%s\n%s", report.ReportID, report.AgentID, report.RunID, status, report.Final, report.Partial, report.FailureCode, report.Text)
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
		if h.Cancel != nil {
			h.Cancel(context.Canceled)
		}
	}
	for _, h := range children {
		if h.Done != nil {
			<-h.Done
		}
	}
}

func (r *Run) ResultForParent() SubagentReport {
	result := SubagentReport{
		ReportID:     identity.NewReportID(),
		AgentID:      r.AgentID(),
		RunID:        r.id,
		ParentRunID:  r.collaboration.parent.ID(),
		ParentCallID: r.parentCallID,
		Outcome:      r.terminal.RunOutcome(),
		Final:        true,
	}
	switch payload := r.terminal.Payload.(type) {
	case sessionentry.RunFailed:
		result.FailureCode = payload.Code
	case sessionentry.RunInterrupted:
		result.FailureCode = sessionentry.RunFailureCode(payload.Reason)
	}
	if index, ok := r.entryIndexes[r.progress.FinalAssistantID]; ok {
		result.Text = r.entries[index].TextContent()
	} else {
		for i := len(r.entries) - 1; i >= 0; i-- {
			request, ok := r.entries[i].Payload.(sessionentry.ModelRequest)
			if !ok {
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
	r.collaboration.mu.Lock()
	r.wakeLocked()
	r.collaboration.mu.Unlock()
}

func (r *Run) wakeLocked() {
	close(r.collaboration.wake)
	r.collaboration.wake = make(chan struct{})
}

// CompleteSubagent 在交付后释放子 Run；保留句柄供父 Run 等待收尾。
func (r *Run) CompleteSubagent(id identity.AgentID) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	h := r.collaboration.children[id]
	h.Run = nil
	r.collaboration.children[id] = h
}
