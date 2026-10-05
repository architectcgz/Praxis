package context

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	sessionmodel "praxis/internal/core/session"
)

const (
	defaultSessionBudget = 16 * 1024
	defaultMessageBudget = 32 * 1024
)

// BuildInput 是 ContextBuilder 的全部输入，调用方负责提供一致性边界内的数据。
type BuildInput struct {
	SystemPrompt            string
	Entries                 []Entry
	MessageSequenceBoundary uint64
	Messages                []sessionmodel.MessageData
	CurrentInput            string
	CurrentInputMessageID   string
}

// BuildResult 返回完整的初始 ModelContext 及其内容摘要。
type BuildResult struct {
	Context                 ModelContext
	Digest                  string
	MessageSequenceBoundary uint64
	CurrentInputMessageID   string
}

// ContextBuilder 按固定优先级和字节预算构建完整 ModelContext。
type ContextBuilder struct {
	sessionBudget int
	messageBudget int
}

// NewContextBuilder 创建 ContextBuilder。预算为零时使用默认值。
func NewContextBuilder(sessionBudget, messageBudget int) ContextBuilder {
	if sessionBudget <= 0 {
		sessionBudget = defaultSessionBudget
	}
	if messageBudget <= 0 {
		messageBudget = defaultMessageBudget
	}
	return ContextBuilder{sessionBudget: sessionBudget, messageBudget: messageBudget}
}

// Build 根据共享条目、消息历史和当前输入生成完整模型上下文。
func (b ContextBuilder) Build(input BuildInput) (BuildResult, error) {
	currentInput := input.CurrentInput
	if len([]byte(currentInput)) > b.sessionBudget {
		return BuildResult{}, errors.New("current input exceeds context budget")
	}
	entries := selectEntries(input.Entries, b.sessionBudget-len([]byte(currentInput)))
	if len(entries) == 0 && len(input.Messages) == 0 && currentInput == "" {
		return BuildResult{}, errors.New("context build selected no session entries")
	}
	var currentMessage sessionmodel.MessageData
	history := make([]sessionmodel.MessageData, 0, len(input.Messages))
	for _, value := range input.Messages {
		if value.ID == input.CurrentInputMessageID {
			currentMessage = value
			continue
		}
		history = append(history, value)
	}
	currentMessageBytes := messageContextBytes(currentMessage)
	if input.CurrentInputMessageID != "" && currentMessage.ID == "" && currentInput == "" {
		return BuildResult{}, errors.New("current input message is missing")
	}
	if currentMessage.ID != "" && currentMessage.Role != sessionmodel.RoleUser {
		return BuildResult{}, errors.New("current input message must have user role")
	}
	if currentMessageBytes > b.messageBudget {
		return BuildResult{}, errors.New("current input message exceeds context budget")
	}
	messages := selectMessages(history, b.messageBudget-currentMessageBytes)

	content := make([]ContextEntry, 0, len(entries)+len(messages)+1)
	var sessionID string
	for _, entry := range entries {
		if err := ValidateEntry(entry); err != nil {
			return BuildResult{}, err
		}
		if sessionID == "" {
			sessionID = entry.SessionID
		} else if sessionID != entry.SessionID {
			return BuildResult{}, errors.New("context entries belong to different sessions")
		}
		content = append(content, ContextEntry{
			Kind: ContextEntrySession,
			Role: ContextRoleUser,
			Content: []ContextBlock{{
				Kind: ContextBlockText,
				Text: fmt.Sprintf("[Session context: %s]\n%s", entry.Kind, entry.Content),
			}},
		})
	}
	for _, message := range messages {
		if entry, ok := EntryFromMessageData(message); ok {
			content = append(content, entry)
		}
	}
	if currentMessage.ID != "" {
		entry, ok := EntryFromMessageData(currentMessage)
		if !ok {
			return BuildResult{}, errors.New("current input message has no model-visible content")
		}
		content = append(content, entry)
	} else if currentInput != "" {
		content = append(content, ContextEntry{
			Kind: ContextEntryUserInput,
			Role: ContextRoleUser,
			Content: []ContextBlock{{
				Kind: ContextBlockText,
				Text: currentInput,
			}},
		})
	}

	modelContext := ModelContext{
		SystemPrompt: input.SystemPrompt,
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
		Context:                 modelContext,
		Digest:                  digest,
		MessageSequenceBoundary: input.MessageSequenceBoundary,
		CurrentInputMessageID:   input.CurrentInputMessageID,
	}, nil
}

func selectEntries(entries []Entry, budget int) []Entry {
	candidates := slices.Clone(entries)
	slices.SortFunc(candidates, func(left, right Entry) int {
		if order := cmp.Compare(entryPriority(left.Kind), entryPriority(right.Kind)); order != 0 {
			return order
		}
		if !right.CreatedAt.Equal(left.CreatedAt) {
			if right.CreatedAt.Before(left.CreatedAt) {
				return 1
			}
			return -1
		}
		return cmp.Compare(right.ID, left.ID)
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
		if !left.CreatedAt.Equal(right.CreatedAt) {
			if left.CreatedAt.Before(right.CreatedAt) {
				return -1
			}
			return 1
		}
		return cmp.Compare(left.ID, right.ID)
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

func selectMessages(messages []sessionmodel.MessageData, budget int) []sessionmodel.MessageData {
	selected := make([]sessionmodel.MessageData, 0, len(messages))
	used := 0
	for end := len(messages); end > 0; {
		start := end - 1
		turnID := messages[end-1].TurnID
		for start > 0 && turnID != "" && messages[start-1].TurnID == turnID {
			start--
		}
		size := 0
		for _, message := range messages[start:end] {
			size += messageContextBytes(message)
		}
		if size <= budget-used {
			combined := make([]sessionmodel.MessageData, 0, end-start+len(selected))
			combined = append(combined, messages[start:end]...)
			selected = append(combined, selected...)
			used += size
		}
		end = start
	}
	return selected
}

func messageContextBytes(value sessionmodel.MessageData) int {
	size := 0
	for _, block := range value.Blocks {
		if block.Kind == sessionmodel.BlockThinking {
			continue
		}
		size += len([]byte(block.Text)) + len(block.CallID) + len(block.Name) + len(block.Input)
	}
	return size
}
