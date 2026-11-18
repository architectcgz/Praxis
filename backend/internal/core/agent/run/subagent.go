package run

import (
	"context"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/tool"
	"praxis/internal/dto"
	"praxis/internal/identity"
	"slices"
	"sync"
)

type Assignment struct {
	Task           string          `json:"task"`
	AllowedTools   []tool.ToolName `json:"allowedTools"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
}

type SubagentHandle struct {
	Result  dto.RunSummary
	AgentID identity.AgentID
	Run     *Run
	Cancel  context.CancelCauseFunc
	Done    <-chan struct{}
}

type SubagentStatus struct {
	AgentID identity.AgentID         `json:"agentId"`
	Run     dto.RunSummary `json:"run"`
}

type instruction struct {
	id   string
	text string
}

type collaboration struct {
	mu           sync.Mutex
	children     map[identity.AgentID]SubagentHandle
	reports      []sessionentry.SubagentResult
	consumed     map[string]bool
	accepted     []sessionentry.SubagentResult
	wake         chan struct{}
	parent       *Run
	spawn        func(context.Context, Assignment, string) (SubagentStatus, error)
	instructions []instruction
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
		statuses[i] = SubagentStatus{AgentID: h.AgentID, Run: h.Result}
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

// receiveInstruction 只把指令交给 Subagent 的运行时协作队列，不直接写 History。
func (r *Run) receiveInstruction(ctx context.Context, text string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.collaboration.mu.Lock()
	if !r.collaboration.accepting {
		r.collaboration.mu.Unlock()
		return "", fmt.Errorf("subagent execution has ended")
	}
	id := r.deps.IDs.New("instruction")
	r.collaboration.instructions = append(r.collaboration.instructions, instruction{id: id, text: text})
	r.collaboration.mu.Unlock()
	r.WakeSubagentWaiters()
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

// acceptInstructions 在 Subagent Loop 的串行点把运行时指令变成普通 Input Entry。
func (r *Run) acceptInstructions(ctx context.Context) (bool, error) {
	r.collaboration.mu.Lock()
	instructions := slices.Clone(r.collaboration.instructions)
	r.collaboration.instructions = nil
	r.collaboration.mu.Unlock()
	if len(instructions) == 0 {
		return false, nil
	}
	for _, instruction := range instructions {
		stored, _, err := r.AppendEntry(ctx, sessionentry.Entry{
			ID: instruction.id,
			Payload: sessionentry.UserInput{
				MessageID: instruction.id,
				Text:      instruction.text,
			},
			Role:       role.User,
			AuthorKind: sessionentry.AuthorAgent,
			Content:    []sessionentry.ContentBlock{sessionentry.TextBlock{Text: instruction.text}},
		})
		if err != nil {
			return false, err
		}
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(""),
			Payload: coreevent.EntriesAppended{Entries: []sessionentry.Entry{*stored}},
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
	result := sessionentry.SubagentResult{
		ReportID:     r.deps.IDs.New("report"),
		AgentID:      r.agentID,
		RunID:        r.id,
		ParentRunID:  r.parentRunID,
		ParentCallID: r.parentCallID,
		Text:         text,
	}
	return parent.AcceptSubagentResult(ctx, result)
}

// AcceptSubagentResult 只更新 Parent Run Memory，不绕过 Parent Loop 写入 History。
func (r *Run) AcceptSubagentResult(ctx context.Context, result sessionentry.SubagentResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.collaboration.mu.Lock()
	r.collaboration.reports = append(r.collaboration.reports, result)
	r.collaboration.mu.Unlock()
	r.WakeSubagentWaiters()
	return nil
}

func (r *Run) WaitSubagents(ctx context.Context, ids []identity.AgentID, all bool) ([]sessionentry.SubagentReceipt, error) {
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
				r.collaboration.accepted = append(r.collaboration.accepted, report)
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

// acceptReports 在 Parent Loop 的串行点把自包含报告变成普通 Input Entry。
func (r *Run) acceptReports(ctx context.Context) error {
	r.collaboration.mu.Lock()
	reports := slices.Clone(r.collaboration.accepted)
	r.collaboration.accepted = nil
	r.collaboration.mu.Unlock()
	for _, report := range reports {
		text := subagentReportText(report)
		stored, _, err := r.AppendEntry(ctx, sessionentry.Entry{
			ID: "report:" + report.ReportID,
			Payload: sessionentry.UserInput{
				MessageID: report.ReportID,
				Text:      text,
			},
			Role:       role.User,
			AuthorKind: sessionentry.AuthorAgent,
			Content:    []sessionentry.ContentBlock{sessionentry.TextBlock{Text: text}},
		})
		if err != nil {
			return err
		}
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(""),
			Payload: coreevent.EntriesAppended{Entries: []sessionentry.Entry{*stored}},
		})
	}
	return nil
}

func subagentReportText(report sessionentry.SubagentResult) string {
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
		h.Cancel(context.Canceled)
	}
	for _, h := range children {
		<-h.Done
	}
}

func (r *Run) ResultForParent() sessionentry.SubagentResult {
	result := sessionentry.SubagentResult{
		ReportID:     r.deps.IDs.New("report"),
		AgentID:      r.agentID,
		RunID:        r.id,
		ParentRunID:  r.parentRunID,
		ParentCallID: r.parentCallID,
		Outcome:      sessionentry.RunOutcome(r.result.Outcome),
		FailureCode:  sessionentry.RunFailureCode(r.result.FailureCode),
		Final:        true,
	}
	if index, ok := r.entryIndexes[r.result.FinalAssistantID]; ok {
		result.Text = r.entries[index].TextContent()
	} else {
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

// CompleteSubagent 在交付和收尾完成后释放子执行正文，只保留状态和必要控制身份。
func (r *Run) CompleteSubagent(id identity.AgentID, result dto.RunSummary) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	h := r.collaboration.children[id]
	h.Run = nil
	h.Result = result
	r.collaboration.children[id] = h
}

func (r *Run) ActiveSubagents() int {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	count := 0
	for _, child := range r.collaboration.children {
		select {
		case <-child.Done:
		default:
			count++
		}
	}
	return count
}

// ReceiveSubagentStatus 接收子任务主动交付的不可变状态，不读取子 Run 的可变字段。
func (r *Run) ReceiveSubagentStatus(id identity.AgentID, snapshot dto.RunSummary) {
	r.collaboration.mu.Lock()
	defer r.collaboration.mu.Unlock()
	handle := r.collaboration.children[id]
	handle.Result = snapshot
	r.collaboration.children[id] = handle
}
