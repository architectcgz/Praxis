package model

import (
	"errors"
	"strings"
)

// ModelSelection identifies the configured model and reasoning level for one execution.
type ModelSelection struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// NewModelSelection normalizes the model selection fields required for routing a model request.
func NewModelSelection(providerID, modelID, reasoningLevel string) (ModelSelection, error) {
	selection := ModelSelection{
		ProviderID:     strings.TrimSpace(providerID),
		ModelID:        strings.TrimSpace(modelID),
		ReasoningLevel: strings.TrimSpace(reasoningLevel),
	}
	if err := selection.Validate(); err != nil {
		return ModelSelection{}, err
	}
	return selection, nil
}

// Validate verifies that a selection is complete and already normalized.
func (s ModelSelection) Validate() error {
	if s.ProviderID == "" {
		return errors.New("model selection provider ID is required")
	}
	if s.ModelID == "" {
		return errors.New("model selection model ID is required")
	}
	if s.ProviderID != strings.TrimSpace(s.ProviderID) || s.ModelID != strings.TrimSpace(s.ModelID) ||
		s.ReasoningLevel != strings.TrimSpace(s.ReasoningLevel) {
		return errors.New("model selection fields must be normalized")
	}
	return nil
}
