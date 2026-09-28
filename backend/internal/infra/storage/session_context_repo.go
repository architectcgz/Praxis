package storage

import (
	contextmodel "praxis/internal/context"
	"praxis/internal/contracts"

	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Store) CurrentSessionContextRevision(ctx context.Context, sessionID contracts.SessionID) (uint64, error) {
	var revision uint64
	if err := s.executor(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) FROM session_context_entries WHERE session_id = ?`, sessionID.String()).Scan(&revision); err != nil {
		return 0, fmt.Errorf("read session context revision: %w", err)
	}
	return revision, nil
}
func (s *Store) AppendSessionContext(ctx context.Context, entry contextmodel.SessionContextEntry, expectedRevision uint64) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	var existingRef string
	err := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM session_context_entries WHERE id = ?`, entry.ID.String()).Scan(&existingRef)
	if err == nil {
		var existing contextmodel.SessionContextEntry
		if err := s.loadDocument(ctx, existingRef, &existing, "session context entry", func() error { return existing.Validate() }); err != nil {
			return err
		}
		if sameSessionContextEntry(existing, entry) {
			return nil
		}
		return contracts.ErrRequestConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read session context entry identity: %w", err)
	}
	current, err := s.CurrentSessionContextRevision(ctx, entry.SessionID)
	if err != nil {
		return err
	}
	if current != expectedRevision || entry.Revision != expectedRevision+1 {
		return contracts.ErrRevisionConflict
	}
	ref, err := s.putDocument(ctx, "session-context", entry.ID.String(), entry)
	if err != nil {
		return err
	}
	err = s.saveMetadata(ctx, `INSERT INTO session_context_entries (id, session_id, revision, kind, source_execution_id, content_digest, created_at, document_ref) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, entry.ID.String(), entry.SessionID.String(), entry.Revision, string(entry.Kind), nullableID(entry.SourceExecutionID), entry.ContentDigest, entry.CreatedAt.UTC().Format(time.RFC3339Nano), ref)
	if err != nil && isConstraintError(err, "session_context_entries.id") {
		return contracts.ErrRequestConflict
	}
	if err != nil && isConstraintError(err, "session_context_entries.session_id, session_context_entries.revision") {
		return contracts.ErrRevisionConflict
	}
	return err
}
func (s *Store) ListSessionContext(ctx context.Context, sessionID contracts.SessionID, afterRevision uint64, limit int) ([]contextmodel.SessionContextEntry, error) {
	rows, err := s.executor(ctx).QueryContext(ctx, `SELECT id, revision, kind, source_execution_id, content_digest, created_at, document_ref FROM session_context_entries WHERE session_id = ? AND revision > ? ORDER BY revision LIMIT ?`, sessionID.String(), afterRevision, targetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list session context entries: %w", err)
	}
	defer rows.Close()
	entries := make([]contextmodel.SessionContextEntry, 0)
	for rows.Next() {
		var id, kind, digest, createdAt, documentRef string
		var revision uint64
		var sourceExecutionID sql.NullString
		if err := rows.Scan(&id, &revision, &kind, &sourceExecutionID, &digest, &createdAt, &documentRef); err != nil {
			return nil, fmt.Errorf("scan session context entry: %w", err)
		}
		var entry contextmodel.SessionContextEntry
		if err := s.loadDocument(ctx, documentRef, &entry, "session context entry", func() error { return entry.Validate() }); err != nil {
			return nil, err
		}
		indexedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse session context creation time: %w", err)
		}
		if entry.ID.String() != id || entry.SessionID != sessionID || entry.Revision != revision ||
			string(entry.Kind) != kind || entry.SourceExecutionID.String() != sourceExecutionID.String ||
			entry.ContentDigest != digest || !entry.CreatedAt.Equal(indexedAt) {
			return nil, errors.New("session context index does not match document")
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session context entries: %w", err)
	}
	return entries, nil
}

func sameSessionContextEntry(left, right contextmodel.SessionContextEntry) bool {
	return left.ID == right.ID && left.SessionID == right.SessionID && left.Revision == right.Revision &&
		left.Kind == right.Kind && left.SourceExecutionID == right.SourceExecutionID &&
		left.ContentDigest == right.ContentDigest && left.Content == right.Content
}

type SessionContextRepository struct{ store *Store }

func (r SessionContextRepository) CurrentRevision(ctx context.Context, id contracts.SessionID) (uint64, error) {
	return r.store.CurrentSessionContextRevision(ctx, id)
}
func (r SessionContextRepository) Append(ctx context.Context, value contextmodel.SessionContextEntry, expected uint64) error {
	return r.store.AppendSessionContext(ctx, value, expected)
}
func (r SessionContextRepository) List(ctx context.Context, id contracts.SessionID, after uint64, limit int) ([]contextmodel.SessionContextEntry, error) {
	return r.store.ListSessionContext(ctx, id, after, limit)
}
func (r SessionContextRepository) GetByID(ctx context.Context, id contracts.ContextEntryID) (contextmodel.SessionContextEntry, bool, error) {
	return r.store.GetSessionContextEntryByID(ctx, id)
}

// GetSessionContextEntryByID resolves an append command's idempotency identity.
func (s *Store) GetSessionContextEntryByID(ctx context.Context, entryID contracts.ContextEntryID) (contextmodel.SessionContextEntry, bool, error) {
	var ref string
	err := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM session_context_entries WHERE id = ?`, entryID.String()).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return contextmodel.SessionContextEntry{}, false, nil
	}
	if err != nil {
		return contextmodel.SessionContextEntry{}, false, fmt.Errorf("read session context entry identity: %w", err)
	}
	var entry contextmodel.SessionContextEntry
	if err := s.loadDocument(ctx, ref, &entry, "session context entry", func() error { return entry.Validate() }); err != nil {
		return contextmodel.SessionContextEntry{}, false, err
	}
	return entry, true, nil
}
