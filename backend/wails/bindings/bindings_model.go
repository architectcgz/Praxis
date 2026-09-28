package bindings

import (
	"praxis/wails/dto"
)

type ModelBindings struct {
	runtime Runtime
}

// ListModels exposes confirmed model labels and declared reasoning
// capabilities without provider endpoints or credentials.
func (b *ModelBindings) ListModels() ([]dto.ModelOption, error) {
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, publicError(b.runtime, "ModelBindings.ListModels.context", err)
	}
	models := service.Models.ListModels()
	result := make([]dto.ModelOption, 0, len(models))
	for _, option := range models {
		result = append(result, dto.ModelOption{
			ProviderID: option.ProviderID, ModelID: option.ModelID, DefaultModelID: option.DefaultModelID, Label: option.Label,
			ProviderName:             option.ProviderName,
			ReasoningLevels:          append([]string{}, option.ReasoningLevels...),
			DefaultReasoningLevel:    option.DefaultReasoningLevel,
			AssignedAgentDefinitions: append([]string{}, option.AssignedAgentDefinitions...),
		})
	}
	return result, nil
}
