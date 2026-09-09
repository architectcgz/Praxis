package sqlite

import (
	"context"
	"fmt"
	domaincontext "praxis/internal/domain/context"
	domainfoundation "praxis/internal/domain/foundation"
	"time"
)

func (s *Store) CurrentSessionContextRevision(ctx context.Context, sessionID domainfoundation.SessionID) (uint64, error) {
	var revision uint64
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(revision), 0) FROM session_context_entries WHERE session_id = ?`,
		sessionID.String(),
	).Scan(&revision)
	if err != nil {
		return 0, fmt.Errorf("read session context revision: %w", err)
	}
	return revision, nil
}

func (s *Store) AppendSessionContext(
	ctx context.Context,
	entry domaincontext.SessionContextEntry,
	expectedRevision uint64,
) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	current, err := s.CurrentSessionContextRevision(ctx, entry.SessionID)
	if err != nil {
		return err
	}
	if current != expectedRevision || entry.Revision != expectedRevision+1 {
		return domainfoundation.ErrRevisionConflict
	}
	payload, err := encodePayload(entry)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO session_context_entries (
			session_id, revision, kind, source_execution_id, created_at, payload
		) VALUES (?, ?, ?, ?, ?, ?)`,
		entry.SessionID.String(), entry.Revision, string(entry.Kind), nullableID(entry.SourceExecutionID),
		entry.CreatedAt.UTC().Format(time.RFC3339Nano), payload,
	)
	if err != nil && isConstraintError(err, "session_context_entries.session_id, session_context_entries.revision") {
		return domainfoundation.ErrRevisionConflict
	}
	return err
}

func (s *Store) ListSessionContext(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
	afterRevision uint64,
	limit int,
) ([]domaincontext.SessionContextEntry, error) {
	return listTargetPayloads[domaincontext.SessionContextEntry](
		ctx,
		s,
		`SELECT payload FROM session_context_entries
		 WHERE session_id = ? AND revision > ? ORDER BY revision LIMIT ?`,
		[]any{sessionID.String(), afterRevision, targetLimit(limit)},
		"session context entries",
		func(value domaincontext.SessionContextEntry) error { return value.Validate() },
	)
}

type SessionContextRepository struct{ store *Store }

func (r SessionContextRepository) CurrentRevision(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
) (uint64, error) {
	return r.store.CurrentSessionContextRevision(ctx, sessionID)
}

func (r SessionContextRepository) Append(
	ctx context.Context,
	entry domaincontext.SessionContextEntry,
	expectedRevision uint64,
) error {
	return r.store.AppendSessionContext(ctx, entry, expectedRevision)
}

func (r SessionContextRepository) List(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
	afterRevision uint64,
	limit int,
) ([]domaincontext.SessionContextEntry, error) {
	return r.store.ListSessionContext(ctx, sessionID, afterRevision, limit)
}
