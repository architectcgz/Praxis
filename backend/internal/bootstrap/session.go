package bootstrap

import (
	"context"
	"errors"
	"sync"

	"praxis/internal/agent"
	"praxis/internal/hook"
	"praxis/internal/identity"
	"praxis/internal/session"
	"praxis/internal/storage/jsonl"
	"praxis/internal/workspace/fs"
)

func buildSessions(resources storageResources, repos jsonl.Repositories, agents *agent.Registry, hooks *hook.Dispatcher, configMu *sync.RWMutex) (*session.Service, error) {
	usages := jsonl.ModelUsageStore{Store: resources.store}
	return session.NewService(session.Dependencies{
		ConfigMu:       configMu,
		Transactions:   resources.store,
		HookDispatcher: hooks,
		Projects:       repos.Projects,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Contexts:       repos.Contexts,
		Agents:         repos.Agents,
		Tasks:          repos.Executions,
		Definitions:    agents.Definition,
		UsageRecords:   usages.ListSession,
		TextReader:     fs.Filesystem{},
		RemoveSessionData: func(ctx context.Context, _ identity.SessionID, refs []string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			var cleanupErr error
			for _, ref := range refs {
				cleanupErr = errors.Join(cleanupErr, resources.documents.Remove(ctx, ref))
			}
			return cleanupErr
		},
	})
}
