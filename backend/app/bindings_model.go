package app

import "praxis/internal/contracts"

type ModelBindings struct {
	runtime *bindingRuntime
	catalog serviceRef[ModelCatalog]
	editor  serviceRef[ModelConfigEditor]
}

func (b *ModelBindings) configEditor() (ModelConfigEditor, error) {
	if _, err := b.runtime.context(); err != nil {
		return nil, err
	}
	editor := b.editor.get()
	if editor == nil {
		return nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	return editor, nil
}

// ListModels exposes confirmed model labels and declared reasoning
// capabilities without provider endpoints or credentials.
func (b *ModelBindings) ListModels() (response []contracts.ModelOption, err error) {
	done := b.runtime.begin("ListModels")
	defer func() { done(err) }()
	if _, err = b.runtime.context(); err != nil {
		return nil, err
	}
	catalog := b.catalog.get()
	if catalog == nil {
		return nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	models := catalog.ListModels()
	result := make([]contracts.ModelOption, 0, len(models))
	for _, model := range models {
		result = append(result, contracts.ModelOption{
			ProviderID: model.ProviderID, ModelID: model.ModelID, Label: model.Label, ProviderName: model.ProviderName,
			Reasoning: contracts.ReasoningOption{
				Supported: model.Reasoning.Supported,
				Levels:    append([]string{}, model.Reasoning.Levels...),
				Default:   model.Reasoning.Default,
			},
			DefaultProfiles: append([]string{}, model.DefaultProfiles...),
		})
	}
	return result, nil
}
