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

// SessionLog 管理一个 Session 下各 Agent 的 JSONL 文件和专属 Writer。
type SessionLog struct {
	sessionID identity.SessionID
	directory string
	backupDir string
	mainAgent identity.AgentID
	paths     map[identity.AgentID]string
	writers   map[identity.AgentID]*jsonl.Writer
}

func (r SessionRepository) OpenLog(ctx context.Context, id identity.SessionID) (*SessionLog, error) {
	info, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	log := &SessionLog{
		sessionID: id,
		directory: sessionPath(r.dataRoot, info.ProjectID, id),
		backupDir: r.dataRoot,
		mainAgent: identity.AgentID(id),
		paths:     make(map[identity.AgentID]string),
		writers:   make(map[identity.AgentID]*jsonl.Writer),
	}
	mainPath := log.path(log.mainAgent)
	if err := log.recoverFile(ctx, mainPath); err != nil {
		return nil, err
	}
	writer, err := jsonl.OpenWriter(ctx, mainPath)
	if err != nil {
		return nil, err
	}
	log.paths[log.mainAgent] = mainPath
	log.writers[log.mainAgent] = writer

	entries, err := os.ReadDir(filepath.Join(log.directory, "subagents"))
	if errors.Is(err, os.ErrNotExist) {
		return log, nil
	}
	if err != nil {
		_ = writer.Close(context.Background())
		return nil, err
	}
	for _, value := range entries {
		name, matched := strings.CutSuffix(value.Name(), ".jsonl")
		if value.IsDir() || !matched {
			continue
		}
		agentID := identity.AgentID(name)
		path := log.path(agentID)
		if err := log.recoverFile(ctx, path); err != nil {
			_ = log.Close(context.Background())
			return nil, err
		}
		subagentWriter, err := jsonl.OpenWriter(ctx, path)
		if err != nil {
			_ = log.Close(context.Background())
			return nil, err
		}
		log.paths[agentID] = path
		log.writers[agentID] = subagentWriter
	}
	return log, nil
}

func (l *SessionLog) recoverFile(ctx context.Context, path string) error {
	if err := jsonl.CheckRegularFile(path); err != nil {
		return err
	}
	return jsonl.ReadFrom(ctx, path, l.backupDir, 0, func(jsonl.Position, []byte) error { return nil })
}

func (l *SessionLog) writer(agentID identity.AgentID) (*jsonl.Writer, error) {
	writer, ok := l.writers[agentID]
	if !ok {
		return nil, fmt.Errorf("history for agent %s is not open", agentID)
	}
	return writer, nil
}

func (l *SessionLog) readBackward(ctx context.Context, agentID identity.AgentID, cursor *int64, limit int) ([]jsonl.Record, *int64, bool, error) {
	path, ok := l.paths[agentID]
	if !ok {
		return nil, nil, false, fmt.Errorf("history for agent %s is not open", agentID)
	}
	return jsonl.ReadBackward(ctx, path, cursor, limit)
}

func (l *SessionLog) CreateSubagent(ctx context.Context, agentID identity.AgentID) error {
	if _, exists := l.writers[agentID]; exists {
		return nil
	}
	path := l.path(agentID)
	if err := jsonl.Create(ctx, path); err != nil {
		return err
	}
	if err := l.recoverFile(ctx, path); err != nil {
		return err
	}
	writer, err := jsonl.OpenWriter(ctx, path)
	if err != nil {
		return err
	}
	l.paths[agentID] = path
	l.writers[agentID] = writer
	return nil
}

func (l *SessionLog) Close(ctx context.Context) error {
	var err error
	for _, writer := range l.writers {
		err = errors.Join(err, writer.Close(ctx))
	}
	return err
}

// Agents 返回已经打开 History 的 Agent 作用域。
func (l *SessionLog) Agents() []identity.AgentID {
	agents := make([]identity.AgentID, 0, len(l.writers))
	for agentID := range l.writers {
		agents = append(agents, agentID)
	}
	return agents
}

func (l *SessionLog) path(agentID identity.AgentID) string {
	if agentID == l.mainAgent {
		return filepath.Join(l.directory, "main.jsonl")
	}
	return filepath.Join(l.directory, "subagents", agentID.String()+".jsonl")
}
