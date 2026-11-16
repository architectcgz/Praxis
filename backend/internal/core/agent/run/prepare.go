package run

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/identity"
)

type SubagentInput struct {
	Task         taskmodel.Task
	AgentID      identity.AgentID
	ParentRunID  identity.RunID
	ParentCallID string
	Definition   agentmodel.AgentDefinition
	Model        modelapi.Config
	Owner        SessionContext
}

func PrepareTask(ctx context.Context, deps Dependencies, input taskmodel.Task, agentID identity.AgentID, definition agentmodel.AgentDefinition, model modelapi.Config, owner SessionContext) (*Run, error) {
	run, err := prepare(ctx, deps, input, agentID, "", "", definition, model, owner)
	return run, err
}

func PrepareSubagent(ctx context.Context, deps Dependencies, input SubagentInput) (*Run, error) {
	if err := input.Owner.CreateSubagent(ctx, input.AgentID); err != nil {
		return nil, err
	}
	return prepare(ctx, deps, input.Task, input.AgentID, input.ParentRunID, input.ParentCallID, input.Definition, input.Model, input.Owner)
}

func prepare(ctx context.Context, deps Dependencies, input taskmodel.Task, agentID identity.AgentID, parentID identity.RunID, parentCall string, definition agentmodel.AgentDefinition, model modelapi.Config, owner SessionContext) (*Run, error) {
	record := sessionentry.NewRunRecord(identity.RunID(deps.IDs.New("run")), input.SessionID, agentID, input.ID, "input:"+input.ID.String(), sessionentry.RunConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             model,
	}, deps.Clock.Now())
	record.AgentName = definition.Name
	record.ParentRunID = parentID
	record.ParentCallID = parentCall
	run := &Run{
		record:       record,
		main:         parentID == "",
		Session:      owner,
		deps:         deps,
		entryIndexes: make(map[string]int),
		appendFact:   owner.BindHistory(agentID),
	}
	run.initCollaboration()
	if _, _, err := run.AppendEntry(ctx, sessionentry.ContextEntry{
		ID:      "prepared:" + record.ID.String(),
		Payload: sessionentry.RunPrepared{Run: record},
	}); err != nil {
		return run, err
	}
	stored, _, err := run.AppendEntry(ctx, sessionentry.ContextEntry{
		ID:         record.InputEntryID,
		Payload:    sessionentry.UserInput{MessageID: record.InputEntryID, Text: input.Prompt},
		Role:       role.User,
		AuthorKind: sessionentry.AuthorUser,
		Content:    []sessionentry.ContentBlock{sessionentry.TextBlock{Text: input.Prompt}},
	})
	if err != nil {
		return run, err
	}
	deps.Emitter.Emit(coreevent.Event{Source: run.EventSource(""), Payload: coreevent.EntriesAppended{Entries: []sessionentry.ContextEntry{*stored}}})
	if hookErr := deps.HookDispatcher.UserInputCommitted(context.WithoutCancel(ctx), hook.EntryCommitted{Identity: runIdentity(run, ""), Entry: *stored}); hookErr != nil {
		deps.Logger.Errorf("user input committed hook failed: %v", hookErr)
	}
	return run, nil
}
