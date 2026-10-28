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
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SessionRepository struct{ storage *Store }

func (r SessionRepository) Get(ctx context.Context, id identity.SessionID) (sessionmodel.Session, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return sessionmodel.Session{}, err
	}
	defer r.storage.mu.RUnlock()
	return r.storage.sessionRecord(ctx, id)
}

// Create 先建立空历史文件，再提交 SQLite 元数据；数据库提交失败允许留下未被索引的空文件。
func (r SessionRepository) Create(ctx context.Context, value sessionmodel.Session) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	if _, err := r.storage.sessionRecord(ctx, value.ID); err == nil {
		return errorcode.ErrRequestConflict
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return err
	}
	path := r.storage.sessionLogPath(value.ID.String(), value.ProjectID.String())
	if err := createLog(ctx, path); err != nil {
		return err
	}
	_, err := r.storage.sessions.ExecContext(ctx, `
		INSERT INTO sessions (id, project_id, workspace_id, title, state, created_at, updated_at, approval_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID.String(), value.ProjectID.String(), value.WorkspaceID.String(), value.Title,
		string(value.State), value.CreatedAt.UnixNano(), value.UpdatedAt.UnixNano(), value.ApprovalMode)
	if err != nil {
		return fmt.Errorf("create session metadata: %w", err)
	}
	delete(r.storage.writeErrors, path)
	r.storage.state.sessions[value.ID] = value
	return nil
}

func (r SessionRepository) List(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", "", limit)
}
func (r SessionRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, id, "", limit)
}
func (r SessionRepository) SearchTitle(ctx context.Context, title string, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", strings.ToLower(title), limit)
}
func (r SessionRepository) find(ctx context.Context, project identity.ProjectID, title string, limit int) ([]sessionmodel.Session, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	rows, err := r.storage.sessions.QueryContext(ctx, `
		SELECT id, project_id, workspace_id, title, state, created_at, updated_at, approval_mode
		FROM sessions WHERE (? = '' OR project_id = ?)
		ORDER BY updated_at DESC, id`, project.String(), project.String())
	if err != nil {
		return nil, fmt.Errorf("list session metadata: %w", err)
	}
	defer rows.Close()
	values := make([]sessionmodel.Session, 0)
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

func (r SessionRepository) HasEntries(ctx context.Context, id identity.SessionID) (bool, error) {
	if err := r.storage.lockSession(ctx, id); err != nil {
		return false, err
	}
	defer r.storage.mu.Unlock()
	return len(r.storage.state.entryOrder[id]) != 0, nil
}
func (r SessionRepository) Rename(ctx context.Context, id identity.SessionID, title string, at time.Time) error {
	return r.updateName(ctx, id, func(value *sessionmodel.Session) { value.Title, value.UpdatedAt = title, at })
}
func (r SessionRepository) NameFromInput(ctx context.Context, id identity.SessionID, content string, at time.Time) error {
	return r.updateName(ctx, id, func(value *sessionmodel.Session) { value.NameFromInput(content, at) })
}
func (r SessionRepository) updateName(ctx context.Context, id identity.SessionID, update func(*sessionmodel.Session)) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	value, err := r.storage.sessionRecord(ctx, id)
	if err != nil {
		return err
	}
	prior := value
	update(&value)
	if value == prior {
		return nil
	}
	if _, err := r.storage.sessions.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		value.Title, value.UpdatedAt.UnixNano(), id.String()); err != nil {
		return fmt.Errorf("update session metadata: %w", err)
	}
	if _, loaded := r.storage.state.sessions[id]; loaded {
		r.storage.state.sessions[id] = value
	}
	return nil
}

// SetApprovalMode 只更新会话授权配置，不重放历史，也不覆盖并发的标题变更。
func (r SessionRepository) SetApprovalMode(ctx context.Context, id identity.SessionID, mode *security.ApprovalMode, at time.Time) (sessionmodel.Session, error) {
	if err := r.storage.lock(ctx); err != nil {
		return sessionmodel.Session{}, err
	}
	defer r.storage.mu.Unlock()
	value, err := r.storage.sessionRecord(ctx, id)
	if err != nil {
		return sessionmodel.Session{}, err
	}
	value.ApprovalMode = mode
	value.UpdatedAt = at
	if _, err := r.storage.sessions.ExecContext(ctx,
		`UPDATE sessions SET approval_mode = ?, updated_at = ? WHERE id = ?`,
		mode, at.UnixNano(), id.String()); err != nil {
		return sessionmodel.Session{}, fmt.Errorf("update session approval mode: %w", err)
	}
	if _, loaded := r.storage.state.sessions[id]; loaded {
		stored := value
		if mode != nil {
			stored.ApprovalMode = new(*mode)
		}
		r.storage.state.sessions[id] = stored
	}
	return value, nil
}

// Delete 以 SQLite 删除为提交点；失败不碰 JSONL，成功后尽力清理历史文件，允许留下孤儿文件。
func (r SessionRepository) Delete(ctx context.Context, id identity.SessionID, onlyEmpty bool) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	value, err := r.storage.sessionRecord(ctx, id)
	if err != nil {
		if onlyEmpty && errors.Is(err, errorcode.ErrNotFound) {
			return nil
		}
		return err
	}
	if onlyEmpty {
		if err := r.storage.loadSessionLocked(ctx, id); err != nil {
			return err
		}
		if len(r.storage.state.entryOrder[id]) != 0 {
			return nil
		}
	}
	if _, err := r.storage.sessions.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id.String()); err != nil {
		return fmt.Errorf("delete session metadata: %w", err)
	}
	path := r.storage.sessionLogPath(id.String(), value.ProjectID.String())
	r.storage.state.removeSession(id)
	delete(r.storage.writeErrors, path)
	_ = os.Remove(path)
	return nil
}

// SessionLogPath 显式访问文件，不触发历史重放。
func (r SessionRepository) SessionLogPath(ctx context.Context, id identity.SessionID) (string, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return "", err
	}
	defer r.storage.mu.RUnlock()
	value, err := r.storage.sessionRecord(ctx, id)
	if err != nil {
		return "", err
	}
	path := r.storage.sessionLogPath(id.String(), value.ProjectID.String())
	if err := checkRegularFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errorcode.ErrNotFound
		}
		return "", err
	}
	return path, nil
}

func openSessionDatabase(ctx context.Context, dataRoot string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataRoot, "sessions.db"))
	if err != nil {
		return nil, fmt.Errorf("open session database: %w", err)
	}
	// Store 统一协调写入；单连接也避免 SQLite 连接间竞争写锁。
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL,
			title TEXT NOT NULL,
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			approval_mode TEXT CHECK (approval_mode IN ('ask', 'auto', 'yolo'))
		);
		CREATE INDEX IF NOT EXISTS sessions_project_updated ON sessions (project_id, updated_at DESC, id);
	`)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("initialize session database: %w", err), db.Close())
	}
	var hasApprovalMode bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pragma_table_info('sessions') WHERE name = 'approval_mode')`).Scan(&hasApprovalMode)
	if err == nil && !hasApprovalMode {
		_, err = db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN approval_mode TEXT CHECK (approval_mode IN ('ask', 'auto', 'yolo'))`)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("initialize session approval mode: %w", err), db.Close())
	}
	return db, nil
}

func (s *Store) sessionRecord(ctx context.Context, id identity.SessionID) (sessionmodel.Session, error) {
	return scanSession(s.sessions.QueryRowContext(ctx, `
		SELECT id, project_id, workspace_id, title, state, created_at, updated_at, approval_mode
		FROM sessions WHERE id = ?`, id.String()))
}

func scanSession(row interface{ Scan(...any) error }) (sessionmodel.Session, error) {
	var value sessionmodel.Session
	var createdAt, updatedAt int64
	var mode sql.NullString
	if err := row.Scan(&value.ID, &value.ProjectID, &value.WorkspaceID, &value.Title, &value.State, &createdAt, &updatedAt, &mode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sessionmodel.Session{}, errorcode.ErrNotFound
		}
		return sessionmodel.Session{}, fmt.Errorf("read session metadata: %w", err)
	}
	value.CreatedAt, value.UpdatedAt = time.Unix(0, createdAt).UTC(), time.Unix(0, updatedAt).UTC()
	if mode.Valid {
		value.ApprovalMode = new(security.ApprovalMode(mode.String))
	}
	if err := value.Validate(); err != nil {
		return sessionmodel.Session{}, err
	}
	return value, nil
}
