package app

import "praxis/internal/contracts"

// ListSessions exposes only session metadata so the UI can discover durable
// workspaces without loading every Agent transcript or aggregate projection.
func (a *App) ListSessions() (response []contracts.SessionSummary, err error) {
	done := a.beginBinding("ListSessions")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return nil, err
	}
	catalog, ok := service.(sessionCatalogService)
	if !ok {
		return nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	sessions, err := catalog.ListSessions(ctx, 100)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, contracts.SessionSummary{
			ID:           session.ID.String(),
			Goal:         session.Goal,
			WorkspaceKey: session.WorkspaceKey,
			CreatedAt:    session.CreatedAt,
			UpdatedAt:    session.UpdatedAt,
		})
	}
	return result, nil
}

// ListModels exposes only confirmed model labels and their declared reasoning
// capabilities. Provider endpoints and credentials remain private.
func (a *App) ListModels() (response []contracts.ModelOption, err error) {
	done := a.beginBinding("ListModels")
	defer func() { done(err) }()
	_, service, err := a.bindingContext()
	if err != nil {
		return nil, err
	}
	catalog, ok := service.(modelCatalogService)
	if !ok {
		return nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	models := catalog.ListModels()
	result := make([]contracts.ModelOption, 0, len(models))
	for _, model := range models {
		result = append(result, contracts.ModelOption{
			ID: model.ID, Label: model.Label, ProviderLabel: model.ProviderLabel,
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
