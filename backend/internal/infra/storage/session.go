package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"praxis/internal/repository"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SessionRepository 通过原子 SQL 更新会话元数据，不持有历史业务状态。
type SessionRepository struct {
	dataRoot string
	database *sql.DB
}

var _ repository.Session = SessionRepository{}

func (r SessionRepository) Get(ctx context.Context, id identity.SessionID) (sessionmodel.SessionInfo, error) {
	return scanSession(r.database.QueryRowContext(ctx, `
		SELECT id, project_id, title, state, created_at, updated_at, approval_mode
		FROM sessions WHERE id = ?`, id.String()))
}

// Create 将会话、唯一根分支与默认选择放入同一个 SQLite 事务。
func (r SessionRepository) Create(ctx context.Context, value sessionmodel.SessionInfo, root sessionmodel.Branch) (err error) {
	if _, err := r.Get(ctx, value.ID); err == nil {
		return errorcode.ErrRequestConflict
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return err
	}
	dir := branchPath(r.dataRoot, value.ProjectID, value.ID, root.ID)
	if err := createBranchLog(ctx, dir); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(dir))
		}
	}()
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sessions (id, project_id, title, state, created_at, updated_at, approval_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		value.ID.String(), value.ProjectID.String(), value.Title,
		string(value.State), value.CreatedAt.UnixNano(), value.UpdatedAt.UnixNano(), value.ApprovalMode)
	if err != nil {
		return fmt.Errorf("create session metadata: %w", err)
	}
	if err := insertBranch(ctx, tx, value.ID, root); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_defaults (session_id, root_branch_id, branch_id) VALUES (?, ?, ?)`,
		value.ID.String(), root.ID.String(), root.ID.String()); err != nil {
		return fmt.Errorf("create session branch selection: %w", err)
	}
	return tx.Commit()
}

func (r SessionRepository) List(ctx context.Context, limit int) ([]sessionmodel.SessionInfo, error) {
	return r.find(ctx, "", "", limit)
}
func (r SessionRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]sessionmodel.SessionInfo, error) {
	return r.find(ctx, id, "", limit)
}
func (r SessionRepository) SearchTitle(ctx context.Context, title string, limit int) ([]sessionmodel.SessionInfo, error) {
	return r.find(ctx, "", strings.ToLower(title), limit)
}
func (r SessionRepository) find(ctx context.Context, project identity.ProjectID, title string, limit int) ([]sessionmodel.SessionInfo, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT id, project_id, title, state, created_at, updated_at, approval_mode
		FROM sessions WHERE (? = '' OR project_id = ?)
		ORDER BY updated_at DESC, id`, project.String(), project.String())
	if err != nil {
		return nil, fmt.Errorf("list session metadata: %w", err)
	}
	defer rows.Close()
	values := make([]sessionmodel.SessionInfo, 0)
	for rows.Next() {
		value, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(strings.ToLower(value.Title), title) {
			continue
		}
		values = append(values, value)
		if limit > 0 && len(values) == limit {
			break
		}
	}
	return values, rows.Err()
}

func (r SessionRepository) UpdateTitle(ctx context.Context, id identity.SessionID, expectedTitle, title string, at time.Time) error {
	result, err := r.database.ExecContext(ctx, `
		UPDATE sessions SET title = ?, updated_at = ?
		WHERE id = ? AND title = ?`,
		title, at.UnixNano(), id.String(), expectedTitle)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	if count == 0 {
		return errorcode.ErrRequestConflict
	}
	return nil
}

// SetApprovalMode 只更新会话授权配置，不重放历史，也不覆盖并发的标题变更。
func (r SessionRepository) SetApprovalMode(ctx context.Context, id identity.SessionID, mode *security.ApprovalMode, at time.Time) (sessionmodel.SessionInfo, error) {
	value, err := scanSession(r.database.QueryRowContext(ctx, `
		UPDATE sessions SET approval_mode = ?, updated_at = ? WHERE id = ?
		RETURNING id, project_id, title, state, created_at, updated_at, approval_mode`,
		mode, at.UnixNano(), id.String()))
	if err != nil {
		return sessionmodel.SessionInfo{}, fmt.Errorf("update session approval mode: %w", err)
	}
	return value, nil
}

// Delete 以 SQLite 删除为提交点；失败不碰 JSONL，成功后尽力清理历史文件，允许留下孤儿文件。
// 调用方负责排除同一会话的历史读写。
func (r SessionRepository) Delete(ctx context.Context, id identity.SessionID) error {
	value, err := scanSession(r.database.QueryRowContext(ctx, `
		DELETE FROM sessions WHERE id = ?
		RETURNING id, project_id, title, state, created_at, updated_at, approval_mode`, id.String()))
	if err != nil {
		return fmt.Errorf("delete session metadata: %w", err)
	}
	_ = os.RemoveAll(sessionPath(r.dataRoot, value.ProjectID, id))
	return nil
}

// SessionLogPath 显式访问文件，不触发历史重放。
func (r SessionRepository) SessionLogPath(ctx context.Context, id identity.SessionID) (string, error) {
	value, err := r.Get(ctx, id)
	if err != nil {
		return "", err
	}
	var branchID identity.BranchID
	if err := r.database.QueryRowContext(ctx, `SELECT branch_id FROM session_defaults WHERE session_id = ?`, id.String()).Scan(&branchID); err != nil {
		return "", fmt.Errorf("read default branch: %w", err)
	}
	path := filepath.Join(branchPath(r.dataRoot, value.ProjectID, id, branchID), "main.jsonl")
	if err := jsonl.CheckRegularFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errorcode.ErrNotFound
		}
		return "", err
	}
	return path, nil
}

func openSessionMetadataDatabase(ctx context.Context, dataRoot string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataRoot, "sessions.db"))
	if err != nil {
		return nil, fmt.Errorf("open session database: %w", err)
	}
	// 单连接避免 SQLite 连接间竞争写锁，结构变更由事务保证原子性。
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `
		PRAGMA foreign_keys = ON;
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			approval_mode TEXT CHECK (approval_mode IN ('ask', 'auto', 'yolo'))
		);
		CREATE INDEX IF NOT EXISTS sessions_project_updated ON sessions (project_id, updated_at DESC, id);
		CREATE TABLE IF NOT EXISTS session_branches (
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			id TEXT NOT NULL,
			name TEXT NOT NULL,
			request_id TEXT NOT NULL,
			source_branch_id TEXT,
			base_entry_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (session_id, id),
			UNIQUE (session_id, name),
			UNIQUE (session_id, request_id),
			FOREIGN KEY (session_id, source_branch_id) REFERENCES session_branches(session_id, id),
			CHECK ((source_branch_id IS NULL AND base_entry_id = '') OR (source_branch_id IS NOT NULL AND base_entry_id <> ''))
		);
		CREATE UNIQUE INDEX IF NOT EXISTS session_branch_root ON session_branches(session_id) WHERE source_branch_id IS NULL;
		CREATE INDEX IF NOT EXISTS session_branch_parent ON session_branches(session_id, source_branch_id);
		CREATE TABLE IF NOT EXISTS session_defaults (
			session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
			root_branch_id TEXT NOT NULL,
			branch_id TEXT NOT NULL,
			FOREIGN KEY (session_id, root_branch_id) REFERENCES session_branches(session_id, id),
			FOREIGN KEY (session_id, branch_id) REFERENCES session_branches(session_id, id)
		);
	`+historySchema)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("initialize session database: %w", err), db.Close())
	}
	return db, nil
}

func scanSession(row interface{ Scan(...any) error }) (sessionmodel.SessionInfo, error) {
	var value sessionmodel.SessionInfo
	var createdAt, updatedAt int64
	var mode sql.NullString
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Title, &value.State, &createdAt, &updatedAt, &mode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sessionmodel.SessionInfo{}, errorcode.ErrNotFound
		}
		return sessionmodel.SessionInfo{}, fmt.Errorf("read session metadata: %w", err)
	}
	value.CreatedAt, value.UpdatedAt = time.Unix(0, createdAt).UTC(), time.Unix(0, updatedAt).UTC()
	if mode.Valid {
		value.ApprovalMode = new(security.ApprovalMode(mode.String))
	}
	if err := value.Validate(); err != nil {
		return sessionmodel.SessionInfo{}, err
	}
	return value, nil
}
