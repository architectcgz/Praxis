package context

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	sessionmodel "praxis/internal/core/session"
)

// BuildInput 是 ContextBuilder 的全部输入，调用方负责提供一致性边界内的数据。
type BuildInput struct {
	ContextWindow           int
	MaxOutputTokens         int
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

// ContextBuilder 构建完整历史，窗口不足时由请求边界调用摘要压缩，不在事务内访问模型。
type ContextBuilder struct{}

// NewContextBuilder 创建无可变预算的 Builder，窗口由每次 Build 的冻结配置提供。
func NewContextBuilder() ContextBuilder {
	return ContextBuilder{}
}

// Build 根据共享条目、消息历史和当前输入生成完整模型上下文。
func (b ContextBuilder) Build(input BuildInput) (BuildResult, error) {
	currentInput := input.CurrentInput
	if input.ContextWindow <= 0 || input.MaxOutputTokens < 0 || input.MaxOutputTokens >= input.ContextWindow {
		return BuildResult{}, errors.New("context window and output reserve are invalid")
	}
	entries := slices.Clone(input.Entries)
	slices.SortFunc(entries, func(left, right Entry) int {
		if !left.CreatedAt.Equal(right.CreatedAt) {
			if left.CreatedAt.Before(right.CreatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(left.ID, right.ID)
	})
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
	if input.CurrentInputMessageID != "" && currentMessage.ID == "" && currentInput == "" {
		return BuildResult{}, errors.New("current input message is missing")
	}
	if currentMessage.ID != "" && currentMessage.Role != sessionmodel.RoleUser {
		return BuildResult{}, errors.New("current input message must have user role")
	}

	content := make([]ContextEntry, 0, len(entries)+len(history)+1)
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
	content = append(content, historyEntries(history)...)
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
	// 当前输入不能通过摘要缩短；历史可以在 loop 中分批摘要，不能在这里静默丢弃。
	last := modelContext.Entries[len(modelContext.Entries)-1]
	if last.Kind == ContextEntryUserInput {
		minimum := ModelContext{
			SystemPrompt: input.SystemPrompt,
			Entries:      []ContextEntry{last},
		}
		tokens, err := minimum.EstimatedTokens()
		if err != nil {
			return BuildResult{}, err
		}
		if tokens > input.ContextWindow-input.MaxOutputTokens {
			return BuildResult{}, &ContextWindowExceededError{
				EstimatedInputTokens: tokens,
				MaxOutputTokens:      input.MaxOutputTokens,
				ContextWindow:        input.ContextWindow,
			}
		}
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
