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

// SessionLog 持有会话目录和已登记的子代理日志路径，不读取业务 Run。
// 路径在加载期或会话同步提交边界登记，追加也由同一提交边界串行调用；定点读取使用调用方已有的位置。
type SessionLog struct {
	index         *historyIndex
	sessionID     identity.SessionID
	directory     string
	backupDir     string
	mainAgent     identity.AgentID
	subagentPaths map[identity.AgentID]string
}

func (r SessionRepository) OpenLog(ctx context.Context, id identity.SessionID) (*SessionLog, error) {
	info, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &SessionLog{
		index:         newHistoryIndex(),
		sessionID:     id,
		directory:     sessionPath(r.dataRoot, info.ProjectID, id),
		backupDir:     r.dataRoot,
		mainAgent:     identity.AgentID(id),
		subagentPaths: make(map[identity.AgentID]string),
	}, nil
}

func (l SessionLog) ReadFrom(ctx context.Context, agentID identity.AgentID, offset int64, recoverTail bool, apply func(jsonl.Position, []byte) error) error {
	path := l.path(agentID)
	if err := jsonl.CheckRegularFile(path); err != nil {
		return err
	}
	if recoverTail {
		return jsonl.ReadFrom(ctx, path, l.backupDir, offset, apply)
	}
	return jsonl.ScanFrom(ctx, path, offset, apply)
}

func (l SessionLog) ReadAt(ctx context.Context, agentID identity.AgentID, position jsonl.Position) ([]byte, error) {
	return jsonl.ReadAt(ctx, l.path(agentID), position)
}

func (l SessionLog) Append(ctx context.Context, agentID identity.AgentID, data []byte) (jsonl.Position, error) {
	if err := ctx.Err(); err != nil {
		return jsonl.Position{}, err
	}
	path := l.subagentPaths[agentID]
	if agentID == l.mainAgent {
		path = l.path(agentID)
	}
	position, err := jsonl.Append(path, data)
	if err != nil {
		return jsonl.Position{}, fmt.Errorf("append log %s: %w", path, err)
	}
	return position, nil
}

func (l SessionLog) CreateSubagent(ctx context.Context, agentID identity.AgentID) error {
	path := l.path(agentID)
	if err := jsonl.Create(ctx, path); err != nil {
		return err
	}
	l.subagentPaths[agentID] = path
	return nil
}

func (l SessionLog) Subagents(ctx context.Context) ([]identity.AgentID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(l.directory, "subagents"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	agents := make([]identity.AgentID, 0, len(entries))
	for _, entry := range entries {
		if name, matched := strings.CutSuffix(entry.Name(), ".jsonl"); !entry.IsDir() && matched {
			agentID := identity.AgentID(name)
			agents = append(agents, agentID)
			l.subagentPaths[agentID] = l.path(agentID)
		}
	}
	return agents, nil
}

func (l SessionLog) path(agentID identity.AgentID) string {
	base := l.directory
	if agentID == l.mainAgent {
		return filepath.Join(base, "main.jsonl")
	}
	return filepath.Join(base, "subagents", agentID.String()+".jsonl")
}
