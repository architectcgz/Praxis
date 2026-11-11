package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"strings"
)

// SessionLog 持有会话目录和已登记的子代理日志路径，不读取业务 Run。
// 路径在加载期或会话同步提交边界登记，追加也由同一提交边界串行调用；定点读取使用调用方已有的位置。
type SessionLog struct {
	database      *sql.DB
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
		database:      r.database,
		sessionID:     id,
		directory:     sessionPath(r.dataRoot, info.ProjectID, id),
		backupDir:     r.dataRoot,
		mainAgent:     identity.AgentID(id),
		subagentPaths: make(map[identity.AgentID]string),
	}, nil
}

func (l SessionLog) ReadFrom(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID, offset int64, recoverTail bool, apply func(jsonl.Position, []byte) error) error {
	path := l.path(branchID, agentID)
	if err := jsonl.CheckRegularFile(path); err != nil {
		return err
	}
	if recoverTail {
		return jsonl.ReadFrom(ctx, path, l.backupDir, offset, apply)
	}
	return jsonl.ScanFrom(ctx, path, offset, apply)
}

func (l SessionLog) ReadAt(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID, position jsonl.Position) ([]byte, error) {
	return jsonl.ReadAt(ctx, l.path(branchID, agentID), position)
}

func (l SessionLog) Append(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID, data []byte) (jsonl.Position, error) {
	if err := ctx.Err(); err != nil {
		return jsonl.Position{}, err
	}
	path := l.subagentPaths[agentID]
	if agentID == l.mainAgent {
		path = l.path(branchID, agentID)
	}
	position, err := jsonl.Append(path, data)
	if err != nil {
		return jsonl.Position{}, fmt.Errorf("append log %s: %w", path, err)
	}
	return position, nil
}

func (l SessionLog) CreateSubagent(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID) error {
	path := l.path(branchID, agentID)
	if err := jsonl.Create(ctx, path); err != nil {
		return err
	}
	l.subagentPaths[agentID] = path
	return nil
}

func (l SessionLog) Subagents(ctx context.Context, branchID identity.BranchID) ([]identity.AgentID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(l.directory, "branches", branchID.String(), "subagents"))
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
			l.subagentPaths[agentID] = l.path(branchID, agentID)
		}
	}
	return agents, nil
}

func (l SessionLog) RemoveBranch(ctx context.Context, branchID identity.BranchID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Join(l.directory, "branches", branchID.String())
	maps.DeleteFunc(l.subagentPaths, func(_ identity.AgentID, path string) bool { return filepath.Dir(filepath.Dir(path)) == directory })
	return os.RemoveAll(directory)
}

func (l SessionLog) path(branchID identity.BranchID, agentID identity.AgentID) string {
	base := filepath.Join(l.directory, "branches", branchID.String())
	if agentID == l.mainAgent {
		return filepath.Join(base, "main.jsonl")
	}
	return filepath.Join(base, "subagents", agentID.String()+".jsonl")
}
