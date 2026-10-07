package bindings

import "praxis/wails/dto"

type ModelBindings struct {
	runtime Runtime
}

// ListModels 返回模型展示信息和推理能力，不暴露 Provider 凭据。
func (b *ModelBindings) ListModels() ([]dto.ModelOption, error) {
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, publicError(b.runtime, "ModelBindings.ListModels.context", err)
	}
	models := services.Models.ListModels()
	result := make([]dto.ModelOption, 0, len(models))
	for _, option := range models {
		result = append(result, dto.ModelOption{
			ProviderID:               option.ProviderID,
			ModelID:                  option.ModelID,
			DefaultModelID:           option.DefaultModelID,
			Label:                    option.Label,
			ProviderName:             option.ProviderName,
			ReasoningLevels:          option.ReasoningLevels,
			DefaultReasoningLevel:    option.DefaultReasoningLevel,
			AssignedAgentDefinitions: option.AssignedAgentDefinitions,
		})
	}
	return result, nil
}
