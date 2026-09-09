package model

import (
	"errors"
	"fmt"
	"strings"
)

const defaultReasoningLevel = "medium"

// Model describes the configured capabilities of one model.
type Model struct {
	ID                    string
	ContextWindow         int
	MaxOutputTokens       int
	ReasoningLevels       []string
	DefaultReasoningLevel string
}

// SelectedModel is the immutable model configuration selected for one execution.
type SelectedModel struct {
	Selection       ModelSelection
	ContextWindow   int
	MaxOutputTokens int
}

// NewModel normalizes and validates model capabilities loaded from configuration.
func NewModel(id string, contextWindow, maxOutputTokens int, reasoningLevels []string, defaultReasoningLevel string) (Model, error) {
	normalizedLevels, normalizedDefault, err := normalizeReasoningLevels(reasoningLevels, defaultReasoningLevel)
	if err != nil {
		return Model{}, err
	}
	model := Model{
		ID:                    strings.TrimSpace(id),
		ContextWindow:         contextWindow,
		MaxOutputTokens:       maxOutputTokens,
		ReasoningLevels:       normalizedLevels,
		DefaultReasoningLevel: normalizedDefault,
	}
	if model.ID == "" {
		return Model{}, errors.New("model ID is required")
	}
	if model.ContextWindow <= 0 || model.MaxOutputTokens <= 0 || model.MaxOutputTokens >= model.ContextWindow {
		return Model{}, errors.New("model capability limits are invalid")
	}
	return model, nil
}

// Select resolves the requested reasoning level into an execution-specific model selection.
func (m Model) Select(providerID, reasoningLevel string) (SelectedModel, error) {
	reasoningLevel = strings.TrimSpace(reasoningLevel)
	if len(m.ReasoningLevels) == 0 {
		if reasoningLevel != "" {
			return SelectedModel{}, fmt.Errorf("model %q does not support reasoning", m.ID)
		}
		selection, err := NewModelSelection(providerID, m.ID, "")
		if err != nil {
			return SelectedModel{}, err
		}
		return SelectedModel{Selection: selection, ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens}, nil
	}
	if reasoningLevel == "" {
		reasoningLevel = m.DefaultReasoningLevel
	}
	if !containsReasoningLevel(m.ReasoningLevels, reasoningLevel) {
		return SelectedModel{}, fmt.Errorf("model %q does not support reasoning %q", m.ID, reasoningLevel)
	}
	selection, err := NewModelSelection(providerID, m.ID, reasoningLevel)
	if err != nil {
		return SelectedModel{}, err
	}
	return SelectedModel{Selection: selection, ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens}, nil
}

func normalizeReasoningLevels(reasoningLevels []string, defaultReasoningLevel string) ([]string, string, error) {
	if len(reasoningLevels) == 0 {
		if strings.TrimSpace(defaultReasoningLevel) != "" {
			return nil, "", errors.New("reasoning default requires levels")
		}
		return nil, "", nil
	}
	levels := make([]string, 0, len(reasoningLevels))
	seen := make(map[string]struct{}, len(reasoningLevels))
	for _, level := range reasoningLevels {
		level = strings.TrimSpace(level)
		if level == "" {
			return nil, "", errors.New("reasoning levels cannot be empty")
		}
		if _, exists := seen[level]; exists {
			return nil, "", fmt.Errorf("duplicate reasoning level %q", level)
		}
		seen[level] = struct{}{}
		levels = append(levels, level)
	}
	defaultLevel := strings.TrimSpace(defaultReasoningLevel)
	if defaultLevel == "" {
		defaultLevel = defaultReasoningLevel
	}
	if !containsReasoningLevel(levels, defaultLevel) {
		return nil, "", fmt.Errorf("reasoning default %q is not supported", defaultLevel)
	}
	return levels, defaultLevel, nil
}

func containsReasoningLevel(levels []string, target string) bool {
	for _, level := range levels {
		if level == target {
			return true
		}
	}
	return false
}
