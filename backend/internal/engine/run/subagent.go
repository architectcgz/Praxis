package run

import (
	"context"
	"fmt"
	"praxis/internal/entry"
	"praxis/internal/event"
	"praxis/internal/identity"
	"slices"
	"sync"
)

type SubagentHandle struct {
	AgentID identity.AgentID
	Run     *Run
	Cancel  context.CancelCauseFunc
	Done    <-chan struct{}
}

// SubagentReport 保留创建者身份，正文由会话内的主 Run 接纳。
type SubagentReport struct {
	ReportID     identity.ReportID
	AgentID      identity.AgentID
	RunID        identity.RunID
	ParentRunID  identity.RunID
	ParentCallID string
	Outcome      entry.RunOutcome
	FailureCode  entry.RunFailureCode
	Text         string
	Final        bool
	Partial      bool
}

type instruction struct {
	id   identity.EntryID
	text string
}

// Subagents 由会话持有子执行控制句柄和待接纳报告，不随某次主 Run 的结束释放。
// 已结束子执行保留轻量句柄供后续等待查询，Run 引用在交付后释放；会话关闭时整体释放。
type Subagents struct {
	mu       sync.Mutex
	children map[identity.AgentID]SubagentHandle
	pending  []SubagentReport
	accepted []SubagentReport
	wake     chan struct{}
}

type collaboration struct {
	mu           sync.Mutex
	instructions []instruction
	accepting    bool
}

func NewSubagents() *Subagents {
	return &Subagents{
		children: make(map[identity.AgentID]SubagentHandle),
		wake:     make(chan struct{}),
	}
}

func (r *Run) initCollaboration() {
	r.collaboration.accepting = true
}

// ReserveSubagent 在会话内原子检查活跃名额；准备阶段也登记取消和完成句柄。
func (s *Subagents) ReserveSubagent(handle SubagentHandle, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeCountLocked() >= limit {
		return fmt.Errorf("session active subagent limit of %d is reached", limit)
	}
	s.children[handle.AgentID] = handle
	return nil
}

func (s *Subagents) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeCountLocked() > 0
}

// HasFinalReports 只关注尚未接纳的终态报告，进展报告不单独启动主 Run。
func (s *Subagents) HasFinalReports() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	final := func(report SubagentReport) bool { return report.Final }
	return slices.ContainsFunc(s.pending, final) || slices.ContainsFunc(s.accepted, final)
}

func (s *Subagents) activeCountLocked() int {
	active := 0
	for _, child := range s.children {
		select {
		case <-child.Done:
		default:
			active++
		}
	}
	return active
}

// AttachSubagent 为已预留的子代理名额绑定运行句柄。
func (s *Subagents) AttachSubagent(handle SubagentHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.children[handle.AgentID] = handle
}

// ReleaseSubagent 移除已经清理完毕的准备失败占位。
func (s *Subagents) ReleaseSubagent(id identity.AgentID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.children, id)
}

func (s *Subagents) CancelSubagent(id identity.AgentID) error {
	s.mu.Lock()
	handle, ok := s.children[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("subagent %s does not belong to this session", id)
	}
	if handle.Cancel == nil {
		return fmt.Errorf("subagent %s is not running", id)
	}
	handle.Cancel(ErrInterrupted)
	return nil
}

// CancelSubagentRun 不依赖创建者 Run 是否仍然活跃。
func (s *Subagents) CancelSubagentRun(id identity.RunID) {
	s.mu.Lock()
	for _, child := range s.children {
		if child.Run != nil && child.Run.ID() == id {
			s.mu.Unlock()
			if !child.Run.TerminalSaved() {
				child.Cancel(ErrInterrupted)
			}
			return
		}
	}
	s.mu.Unlock()
}

func (s *Subagents) SendToSubagent(ctx context.Context, id identity.AgentID, text string) (identity.EntryID, error) {
	s.mu.Lock()
	handle, ok := s.children[id]
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("subagent %s does not belong to this session", id)
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
	id := identity.NewEntryID()
	r.collaboration.mu.Lock()
	if !r.collaboration.accepting {
		r.collaboration.mu.Unlock()
		return "", fmt.Errorf("subagent execution has ended")
	}
	r.collaboration.instructions = append(r.collaboration.instructions, instruction{
		id:   id,
		text: text,
	})
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
		stored, _, err := r.AppendEntry(ctx, entry.Entry{
			ID:         instruction.id,
			AuthorKind: entry.AuthorAgent,
			Payload: entry.Message{
				Role:    entry.RoleUser,
				Content: []entry.ContentBlock{entry.TextBlock{Text: instruction.text}},
			},
		})
		if err != nil {
			return false, err
		}
		r.deps.Emitter.Emit(event.Event{
			Source:  r.EventSource(""),
			Payload: event.EntriesAppended{Entries: []*entry.Entry{stored}},
		})
	}
	return true, nil
}

// ReportToParent 投递到会话报告队列，由当前或后续主 Run 的 Loop 负责持久化。
func (r *Run) ReportToParent(ctx context.Context, text string) error {
	if r.Main() {
		return fmt.Errorf("this run has no parent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result := SubagentReport{
		ReportID:     identity.NewReportID(),
		AgentID:      r.AgentID(),
		RunID:        r.id,
		ParentRunID:  r.parentRunID,
		ParentCallID: r.parentCallID,
		Text:         text,
	}
	return r.subagents.AcceptSubagentResult(ctx, result)
}

// AcceptSubagentResult 只更新会话报告队列，不绕过主 Run 的 Loop 写入 History。
func (s *Subagents) AcceptSubagentResult(ctx context.Context, result SubagentReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.pending = append(s.pending, result)
	s.wakeLocked()
	s.mu.Unlock()
	return nil
}

func (s *Subagents) WaitSubagents(ctx context.Context, ids []identity.AgentID, all bool) ([]SubagentReport, error) {
	wanted := make(map[identity.AgentID]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	var fresh []SubagentReport
	for {
		s.mu.Lock()
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		finished := true
		for _, id := range ids {
			h, ok := s.children[id]
			if !ok {
				s.mu.Unlock()
				return nil, fmt.Errorf("unknown subagent %s", id)
			}
			select {
			case <-h.Done:
			default:
				finished = false
			}
		}
		if finished || !all {
			s.pending = slices.DeleteFunc(s.pending, func(report SubagentReport) bool {
				if !wanted[report.AgentID] {
					return false
				}
				fresh = append(fresh, report)
				return true
			})
			if finished || len(fresh) > 0 {
				s.accepted = append(s.accepted, fresh...)
				s.mu.Unlock()
				break
			}
		}
		wake := s.wake
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wake:
		}
	}
	return fresh, nil
}

// acceptReports 在主 Loop 的串行点接纳报告，创建者 Run 结束后的报告由后续主 Run 接收。
func (r *Run) acceptReports(ctx context.Context) error {
	if !r.Main() {
		return nil
	}
	r.subagents.mu.Lock()
	reports := append(r.subagents.accepted, r.subagents.pending...)
	r.subagents.accepted = nil
	r.subagents.pending = nil
	r.subagents.mu.Unlock()
	for index, report := range reports {
		text := subagentReportText(report)
		stored, _, err := r.AppendEntry(ctx, entry.Entry{
			ID:         identity.NewEntryID(),
			AuthorKind: entry.AuthorAgent,
			Payload: entry.Message{
				Role:    entry.RoleUser,
				Content: []entry.ContentBlock{entry.TextBlock{Text: text}},
			},
		})
		if err != nil {
			r.subagents.mu.Lock()
			r.subagents.accepted = append(reports[index:], r.subagents.accepted...)
			r.subagents.mu.Unlock()
			return err
		}
		// 报告驱动的主 Run 直接使用首条真实报告作为输入，不生成占位消息。
		if r.input == nil {
			r.input = stored
		}
		r.deps.Emitter.Emit(event.Event{
			Source:  r.EventSource(""),
			Payload: event.EntriesAppended{Entries: []*entry.Entry{stored}},
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

// CancelAll 仅由会话或应用关闭流程调用，不与某次主 Run 的收尾绑定。
func (s *Subagents) CancelAll(cause error) {
	s.mu.Lock()
	cancels := make([]context.CancelCauseFunc, 0, len(s.children))
	for _, child := range s.children {
		if child.Cancel != nil {
			cancels = append(cancels, child.Cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel(cause)
	}
}

// Wait 在调用方已禁止新子执行后等待准备、执行和结果交付完成。
func (s *Subagents) Wait(ctx context.Context) error {
	s.mu.Lock()
	done := make([]<-chan struct{}, 0, len(s.children))
	for _, child := range s.children {
		done = append(done, child.Done)
	}
	s.mu.Unlock()
	for _, signal := range done {
		select {
		case <-signal:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (r *Run) ResultForParent() SubagentReport {
	result := SubagentReport{
		ReportID:     identity.NewReportID(),
		AgentID:      r.AgentID(),
		RunID:        r.id,
		ParentRunID:  r.parentRunID,
		ParentCallID: r.parentCallID,
		Outcome:      r.terminal.RunOutcome(),
		Final:        true,
	}
	switch payload := r.terminal.Payload.(type) {
	case entry.RunFailed:
		result.FailureCode = payload.Code
	case entry.RunInterrupted:
		result.FailureCode = entry.RunFailureCode(payload.Reason)
	}
	if index, ok := r.entryIndexes[r.progress.FinalAssistantID]; ok {
		result.Text = r.entries[index].TextContent()
	} else {
		for i := len(r.entries) - 1; i >= 0; i-- {
			request, ok := r.entries[i].Payload.(entry.ModelRequest)
			if !ok {
				continue
			}
			for _, b := range request.Partial {
				if text, ok := b.(entry.TextBlock); ok {
					result.Text += text.Text
				}
			}
			if result.Text != "" {
				result.Partial = true
				break
			}
		}
	}
	if result.Outcome == entry.OutcomeInterrupted {
		result.Text = "Subagent execution was stopped. Do not restart or retry this assignment without a new user request.\n" + result.Text
	} else if result.Text == "" {
		if result.Outcome == entry.OutcomeFailed {
			result.Text = fmt.Sprintf("Subagent execution failed (%s) before producing a final answer. See the subagent history for completed tool calls and partial results.", result.FailureCode)
		} else {
			result.Text = "Subagent execution ended without a final textual answer. See the subagent history for tool results."
		}
	}
	return result
}

func (s *Subagents) WakeSubagentWaiters() {
	s.mu.Lock()
	s.wakeLocked()
	s.mu.Unlock()
}

func (s *Subagents) wakeLocked() {
	close(s.wake)
	s.wake = make(chan struct{})
}

// CompleteSubagent 在交付后释放运行引用，完成信号仍保留供会话等待收尾。
func (s *Subagents) CompleteSubagent(id identity.AgentID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.children[id]
	h.Run = nil
	h.Cancel = nil
	s.children[id] = h
}
