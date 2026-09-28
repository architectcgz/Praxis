package context

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	defaultSessionBudget    = 16 * 1024
	defaultTranscriptBudget = 32 * 1024
)

// BuildInput 是 ContextBuilder 的全部输入，调用方负责提供一致性边界内的数据。
type BuildInput struct {
	SystemPrompt              string
	SessionRevision           uint64
	Entries                   []Entry
	TranscriptThroughSequence uint64
	TranscriptMessages        []TranscriptMessage
	CurrentInput              string
}

// BuildResult 返回完整的初始 ExecutionContext 及其内容摘要。
type BuildResult struct {
	Context                   ExecutionContext
	Digest                    string
	SessionRevision           uint64
	TranscriptThroughSequence uint64
}

// ContextBuilder 按固定优先级和字节预算构建完整 ExecutionContext。
type ContextBuilder struct {
	sessionBudget    int
	transcriptBudget int
}

// NewContextBuilder 创建 ContextBuilder。预算为零时使用默认值。
func NewContextBuilder(sessionBudget, transcriptBudget int) ContextBuilder {
	if sessionBudget <= 0 {
		sessionBudget = defaultSessionBudget
	}
	if transcriptBudget <= 0 {
		transcriptBudget = defaultTranscriptBudget
	}
	return ContextBuilder{sessionBudget: sessionBudget, transcriptBudget: transcriptBudget}
}

// Build 根据共享条目、transcript 历史和当前输入生成完整模型上下文。
func (b ContextBuilder) Build(input BuildInput) (BuildResult, error) {
	if input.SessionRevision == 0 || input.TranscriptThroughSequence == 0 {
		return BuildResult{}, errors.New("context build requires session and transcript revisions")
	}
	currentInput := strings.TrimSpace(input.CurrentInput)
	if len([]byte(currentInput)) > b.sessionBudget {
		return BuildResult{}, errors.New("current input exceeds context budget")
	}
	entries := selectEntries(input.Entries, b.sessionBudget-len([]byte(currentInput)))
	if len(entries) == 0 && currentInput == "" {
		return BuildResult{}, errors.New("context build selected no session entries")
	}
	messages := selectTranscriptMessages(input.TranscriptMessages, b.transcriptBudget)

	content := make([]ContextEntry, 0, len(entries)+len(messages)+1)
	var sessionID string
	var previousRevision uint64
	for _, entry := range entries {
		if err := ValidateEntry(entry, input.SessionRevision, previousRevision); err != nil {
			return BuildResult{}, err
		}
		if sessionID == "" {
			sessionID = entry.SessionID
		} else if sessionID != entry.SessionID {
			return BuildResult{}, errors.New("context entries belong to different sessions")
		}
		previousRevision = entry.Revision
		content = append(content, ContextEntry{
			Kind: ContextEntrySession,
			Role: ContextRoleUser,
			Content: []ContextBlock{{
				Kind: ContextBlockText,
				Text: fmt.Sprintf("[Session context: %s, revision %d]\n%s", entry.Kind, entry.Revision, entry.Content),
			}},
		})
	}
	for _, message := range messages {
		if entry, ok := EntryFromTranscriptMessage(message); ok {
			content = append(content, entry)
		}
	}
	if currentInput != "" {
		content = append(content, ContextEntry{
			Kind: ContextEntryUserInput,
			Role: ContextRoleUser,
			Content: []ContextBlock{{
				Kind: ContextBlockText,
				Text: currentInput,
			}},
		})
	}

	modelContext := ExecutionContext{
		SystemPrompt: strings.TrimSpace(input.SystemPrompt),
		Entries:      content,
	}
	if err := modelContext.Validate(); err != nil {
		return BuildResult{}, err
	}
	digest, err := modelContext.Digest()
	if err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		Context:                   modelContext,
		Digest:                    digest,
		SessionRevision:           input.SessionRevision,
		TranscriptThroughSequence: input.TranscriptThroughSequence,
	}, nil
}

func selectEntries(entries []Entry, budget int) []Entry {
	candidates := slices.Clone(entries)
	slices.SortFunc(candidates, func(left, right Entry) int {
		if order := cmp.Compare(entryPriority(left.Kind), entryPriority(right.Kind)); order != 0 {
			return order
		}
		return cmp.Compare(right.Revision, left.Revision)
	})
	selected := make([]Entry, 0, len(candidates))
	used := 0
	for _, entry := range candidates {
		size := len([]byte(entry.Content))
		if size > budget-used {
			continue
		}
		selected = append(selected, entry)
		used += size
	}
	slices.SortFunc(selected, func(left, right Entry) int {
		return cmp.Compare(left.Revision, right.Revision)
	})
	return selected
}

func entryPriority(kind EntryKind) int {
	switch kind {
	case EntryDecision:
		return 0
	case EntryAcceptedConclusion:
		return 1
	case EntryReference:
		return 2
	default:
		return 3
	}
}

func selectTranscriptMessages(messages []TranscriptMessage, budget int) []TranscriptMessage {
	selected := make([]TranscriptMessage, 0, len(messages))
	used := 0
	for end := len(messages); end > 0; {
		start := end - 1
		for start > 0 && messages[start-1].ExecutionID == messages[end-1].ExecutionID {
			start--
		}
		size := 0
		for _, message := range messages[start:end] {
			size += transcriptMessageBytes(message)
		}
		if size <= budget-used {
			combined := make([]TranscriptMessage, 0, end-start+len(selected))
			combined = append(combined, messages[start:end]...)
			selected = append(combined, selected...)
			used += size
		}
		end = start
	}
	return selected
}

func transcriptMessageBytes(message TranscriptMessage) int {
	if message.InputBytes > 0 {
		return len([]byte(message.Content)) + message.InputBytes
	}
	return len([]byte(message.Content))
}
