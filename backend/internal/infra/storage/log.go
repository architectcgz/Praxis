package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"strings"
)

// SessionLog 只定位主、子代理日志并读写完整行，不解释业务事件。
type SessionLog struct{ sessions SessionRepository }

func (l SessionLog) Read(ctx context.Context, id identity.SessionID, agentID identity.AgentID, apply func([]byte) error) error {
	path, err := l.path(ctx, id, agentID)
	if err != nil {
		return err
	}
	if err := jsonl.CheckRegularFile(path); err != nil {
		return err
	}
	return jsonl.Read(ctx, path, l.sessions.dataRoot, apply)
}

func (l SessionLog) Append(ctx context.Context, id identity.SessionID, agentID identity.AgentID, data []byte) error {
	path, err := l.path(ctx, id, agentID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := jsonl.Append(path, data); err != nil {
		return fmt.Errorf("append log %s: %w", path, err)
	}
	return nil
}

func (l SessionLog) CreateSubagent(ctx context.Context, id identity.SessionID, agentID identity.AgentID) error {
	path, err := l.path(ctx, id, agentID)
	if err != nil {
		return err
	}
	return jsonl.Create(ctx, path)
}

func (l SessionLog) Subagents(ctx context.Context, id identity.SessionID) ([]identity.AgentID, error) {
	session, err := l.sessions.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(subagentsPath(l.sessions.dataRoot, session.ProjectID, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	agents := make([]identity.AgentID, 0, len(entries))
	for _, entry := range entries {
		name, matched := strings.CutPrefix(entry.Name(), "agent-")
		if !entry.IsDir() && matched && strings.HasSuffix(name, ".jsonl") {
			agents = append(agents, identity.AgentID(strings.TrimSuffix(name, ".jsonl")))
		}
	}
	return agents, nil
}

func (l SessionLog) path(ctx context.Context, id identity.SessionID, agentID identity.AgentID) (string, error) {
	session, err := l.sessions.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if agentID == identity.AgentID(id) {
		return sessionLogPath(l.sessions.dataRoot, session.ProjectID, id), nil
	}
	return filepath.Join(subagentsPath(l.sessions.dataRoot, session.ProjectID, id), "agent-"+agentID.String()+".jsonl"), nil
}

func subagentsPath(dataRoot string, projectID identity.ProjectID, sessionID identity.SessionID) string {
	return filepath.Join(dataRoot, "projects", projectID.String(), "sessions", sessionID.String(), "subagents")
}
