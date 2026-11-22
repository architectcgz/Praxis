package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/engine/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/model"
	"praxis/internal/security"
	"praxis/internal/storage/jsonl"
	"time"

	_ "modernc.org/sqlite"
)

// SessionRepository 通过原子 SQL 更新会话元数据，不持有历史业务状态。
type SessionRepository struct {
	dataRoot string
	database *sql.DB
}

func (r SessionRepository) Get(ctx context.Context, id identity.SessionID) (session.SessionInfo, error) {
	return scanSession(r.database.QueryRowContext(ctx, `
		SELECT id, project_id, title, state, created_at, updated_at, approval_mode, provider_id, model_id, reasoning_level
		FROM sessions WHERE id = ?`, id.String()))
}

// Create 保存会话元数据并创建连续主日志。
func (r SessionRepository) Create(ctx context.Context, value session.SessionInfo) (err error) {
	if _, err := r.Get(ctx, value.ID); err == nil {
		return errorcode.ErrRequestConflict
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return err
	}
	dir := sessionPath(r.dataRoot, value.ProjectID, value.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := jsonl.Create(ctx, filepath.Join(dir, "main.jsonl")); err != nil {
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
		INSERT INTO sessions (id, project_id, title, state, created_at, updated_at, approval_mode, provider_id, model_id, reasoning_level)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID.String(), value.ProjectID.String(), value.Title,
		string(value.State), value.CreatedAt.UnixNano(), value.UpdatedAt.UnixNano(), value.ApprovalMode, value.ModelChoice.ProviderID, value.ModelChoice.ModelID, value.ModelChoice.ReasoningLevel)
	if err != nil {
		return fmt.Errorf("create session metadata: %w", err)
	}

	return tx.Commit()
}

func (r SessionRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]session.SessionInfo, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT id, project_id, title, state, created_at, updated_at, approval_mode, provider_id, model_id, reasoning_level
		FROM sessions WHERE project_id = ?
		ORDER BY updated_at DESC, id`, id.String())
	if err != nil {
		return nil, fmt.Errorf("list session metadata: %w", err)
	}
	defer rows.Close()
	values := make([]session.SessionInfo, 0)
	for rows.Next() {
		value, err := scanSession(rows)
		if err != nil {
			return nil, err
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
func (r SessionRepository) SetApprovalMode(ctx context.Context, id identity.SessionID, mode *security.ApprovalMode, at time.Time) (session.SessionInfo, error) {
	value, err := scanSession(r.database.QueryRowContext(ctx, `
		UPDATE sessions SET approval_mode = ?, updated_at = ? WHERE id = ?
		RETURNING id, project_id, title, state, created_at, updated_at, approval_mode, provider_id, model_id, reasoning_level`,
		mode, at.UnixNano(), id.String()))
	if err != nil {
		return session.SessionInfo{}, fmt.Errorf("update session approval mode: %w", err)
	}
	return value, nil
}

// Delete 以 SQLite 删除为提交点；失败不碰 JSONL，成功后尽力清理历史文件，允许留下孤儿文件。
// 调用方负责排除同一会话的历史读写。
func (r SessionRepository) Delete(ctx context.Context, id identity.SessionID) error {
	value, err := scanSession(r.database.QueryRowContext(ctx, `
		DELETE FROM sessions WHERE id = ?
		RETURNING id, project_id, title, state, created_at, updated_at, approval_mode, provider_id, model_id, reasoning_level`, id.String()))
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
	path := filepath.Join(sessionPath(r.dataRoot, value.ProjectID, id), "main.jsonl")
	if err := jsonl.CheckRegularFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errorcode.ErrNotFound
		}
		return "", err
	}
	return path, nil
}

// IsEmpty 仅检查主日志是否为空文件，不打开 Writer 或触发历史恢复。
func (r SessionRepository) IsEmpty(ctx context.Context, id identity.SessionID) (bool, error) {
	path, err := r.SessionLogPath(ctx, id)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.Size() == 0, nil
}

func openSessionMetadataDatabase(ctx context.Context, dataRoot string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataRoot, "sessions.db"))
	if err != nil {
		return nil, fmt.Errorf("open session database: %w", err)
	}
	// 单连接避免 SQLite 连接间竞争写锁，结构变更由事务保证原子性。
	db.SetMaxOpenConns(1)
	var hasSessions int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sessions'").Scan(&hasSessions); err != nil {
		_ = db.Close()
		return nil, err
	}
	if hasSessions > 0 {
		var columns int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('sessions') WHERE name IN ('provider_id','model_id','reasoning_level')").Scan(&columns); err != nil {
			_ = db.Close()
			return nil, err
		}
		if columns != 3 {
			_ = db.Close()
			return nil, fmt.Errorf("session database format is unsupported; select a new data directory")
		}
	}

	_, err = db.ExecContext(ctx, `
		PRAGMA foreign_keys = ON;
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			approval_mode TEXT CHECK (approval_mode IN ('ask', 'auto', 'yolo')),
			provider_id TEXT NOT NULL DEFAULT '',
			model_id TEXT NOT NULL DEFAULT '',
			reasoning_level TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS sessions_project_updated ON sessions (project_id, updated_at DESC, id);
	`)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("initialize session database: %w", err), db.Close())
	}
	return db, nil
}

func scanSession(row interface{ Scan(...any) error }) (session.SessionInfo, error) {
	var value session.SessionInfo
	var createdAt, updatedAt int64
	var mode sql.NullString
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Title, &value.State, &createdAt, &updatedAt, &mode, &value.ModelChoice.ProviderID, &value.ModelChoice.ModelID, &value.ModelChoice.ReasoningLevel); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return session.SessionInfo{}, errorcode.ErrNotFound
		}
		return session.SessionInfo{}, fmt.Errorf("read session metadata: %w", err)
	}
	value.CreatedAt, value.UpdatedAt = time.Unix(0, createdAt).UTC(), time.Unix(0, updatedAt).UTC()
	if mode.Valid {
		value.ApprovalMode = new(security.ApprovalMode(mode.String))
	}
	if err := value.Validate(); err != nil {
		return session.SessionInfo{}, err
	}
	return value, nil
}

func sessionPath(dataRoot string, projectID identity.ProjectID, sessionID identity.SessionID) string {
	return filepath.Join(dataRoot, "projects", projectID.String(), "sessions", sessionID.String())
}

func (r SessionRepository) SetModelChoice(ctx context.Context, id identity.SessionID, choice model.Choice, at time.Time) error {
	_, err := r.database.ExecContext(ctx, `UPDATE sessions SET provider_id=?,model_id=?,reasoning_level=?,updated_at=? WHERE id=?`, choice.ProviderID, choice.ModelID, choice.ReasoningLevel, at.UnixNano(), id)
	return err
}
