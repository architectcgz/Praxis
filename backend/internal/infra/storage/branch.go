package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"time"
)

func (r SessionRepository) BranchCatalog(ctx context.Context, id identity.SessionID) (sessionmodel.BranchCatalog, error) {
	var catalog sessionmodel.BranchCatalog
	err := r.database.QueryRowContext(ctx, `
		SELECT root_branch_id, branch_id FROM session_defaults WHERE session_id = ?`, id.String()).
		Scan(&catalog.RootBranchID, &catalog.DefaultBranchID)
	if errors.Is(err, sql.ErrNoRows) {
		return catalog, errors.New("session has no registered branch catalog; restore a matching program and data backup")
	}
	if err != nil {
		return catalog, fmt.Errorf("read session branch selection: %w", err)
	}
	rows, err := r.database.QueryContext(ctx, `
		SELECT b.id,b.name,b.request_id,b.source_branch_id,b.base_entry_id,b.created_at,
		 COALESCE((SELECT e.id FROM history_entries e WHERE e.session_id=b.session_id AND e.branch_id=b.id AND e.agent_id=b.session_id ORDER BY e.offset DESC LIMIT 1),b.base_entry_id),
		 (SELECT e.metadata FROM history_entries e WHERE e.session_id=b.session_id AND e.branch_id=b.id AND e.agent_id=b.session_id ORDER BY e.offset DESC LIMIT 1)
		FROM session_branches b WHERE session_id = ? ORDER BY created_at, id`, id.String())
	if err != nil {
		return catalog, fmt.Errorf("read branch catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value sessionmodel.Branch
		var source sql.NullString
		var createdAt int64
		var headMetadata sql.NullString
		if err := rows.Scan(&value.ID, &value.Name, &value.RequestID, &source, &value.BaseEntryID, &createdAt, &value.HeadEntryID, &headMetadata); err != nil {
			return catalog, fmt.Errorf("read branch metadata: %w", err)
		}
		value.SourceBranchID = identity.BranchID(source.String)
		value.CreatedAt = time.Unix(0, createdAt).UTC()
		value.UpdatedAt = value.CreatedAt
		if headMetadata.Valid {
			node, err := decodeNode(headMetadata.String)
			if err != nil {
				return catalog, err
			}
			value.UpdatedAt = node.CreatedAt
		}
		if err := value.Validate(); err != nil {
			return catalog, fmt.Errorf("read branch %s: %w", value.ID, err)
		}
		catalog.Branches = append(catalog.Branches, value)
	}
	return catalog, rows.Err()
}

// CreateBranch 先建立独立空日志，以 SQLite 事务为结构提交点，不复制祖先正文。
func (r SessionRepository) CreateBranch(ctx context.Context, id identity.SessionID, branch sessionmodel.Branch) (err error) {
	info, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	dir := branchPath(r.dataRoot, info.ProjectID, id, branch.ID)
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
	if err := insertBranch(ctx, tx, id, branch); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE session_defaults SET branch_id = ? WHERE session_id = ?`, branch.ID.String(), id.String()); err != nil {
		return fmt.Errorf("select created branch: %w", err)
	}
	return tx.Commit()
}

func (r SessionRepository) SelectBranch(ctx context.Context, id identity.SessionID, branchID identity.BranchID) error {
	result, err := r.database.ExecContext(ctx, `
		UPDATE session_defaults SET branch_id = ? WHERE session_id = ?
		AND EXISTS (SELECT 1 FROM session_branches WHERE session_id = ? AND id = ?)`,
		branchID.String(), id.String(), id.String(), branchID.String())
	if err != nil {
		return fmt.Errorf("select session branch: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errorcode.ErrNotFound
	}
	return nil
}

// DeleteBranch 将默认选择、结构与所属投影放在同一事务，物理目录在提交后清理。
func (r SessionRepository) DeleteBranch(ctx context.Context, id identity.SessionID, branchID, selected identity.BranchID) error {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE session_defaults SET branch_id=? WHERE session_id=?`, selected, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM session_branches WHERE session_id=? AND id=?
	 AND source_branch_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM session_branches child WHERE child.session_id=? AND child.source_branch_id=?)`, id, branchID, id, branchID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errorcode.ErrRequestConflict
	}
	return tx.Commit()
}

func insertBranch(ctx context.Context, tx *sql.Tx, id identity.SessionID, branch sessionmodel.Branch) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_branches (session_id, id, name, request_id, source_branch_id, base_entry_id, created_at)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		id.String(), branch.ID.String(), branch.Name, branch.RequestID,
		branch.SourceBranchID.String(), branch.BaseEntryID, branch.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("create branch metadata: %w", err)
	}
	return nil
}

func createBranchLog(ctx context.Context, dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	if err := jsonl.Create(ctx, filepath.Join(dir, "main.jsonl")); err != nil {
		return errors.Join(err, os.RemoveAll(dir))
	}
	return nil
}

func sessionPath(dataRoot string, projectID identity.ProjectID, sessionID identity.SessionID) string {
	return filepath.Join(dataRoot, "projects", projectID.String(), "sessions", sessionID.String())
}

func branchPath(dataRoot string, projectID identity.ProjectID, sessionID identity.SessionID, branchID identity.BranchID) string {
	return filepath.Join(sessionPath(dataRoot, projectID, sessionID), "branches", branchID.String())
}
