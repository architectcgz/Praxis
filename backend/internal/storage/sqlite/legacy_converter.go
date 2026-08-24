package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"praxis/internal/core/domain"
)

const defaultLegacyConverterVersion = "target-orchestration-v1"

type LegacyConversionOptions struct {
	ConfirmedStopped bool
	SourceSnapshotID string
	BackupLocation   string
	ConverterVersion string
	At               time.Time
}

// LegacyConversionReport contains identifiers, counts, and classifications
// only. Prompt, transcript, attachment, and queue body contents are excluded.
type LegacyConversionReport struct {
	ID                  string
	SourceSnapshotID    string
	ConverterVersion    string
	BackupLocation      string
	ConvertedAt         time.Time
	ConvertedSessions   int
	ConvertedGroups     int
	ConvertedAgents     int
	ConvertedExecutions int
	ConvertedQueuedWork int
	SkippedSessions     int
	LegacyQueueItems    int
	Digest              string
}

// ConvertLegacy migrates a stopped pre-target SQLite snapshot into the target
// tables. It never interprets legacy runtime queue content as a user input or
// context delivery and refuses to overwrite an already converted Session.
func (s *Store) ConvertLegacy(ctx context.Context, options LegacyConversionOptions) (LegacyConversionReport, error) {
	if ctx == nil {
		return LegacyConversionReport{}, errors.New("legacy conversion context is required")
	}
	if !options.ConfirmedStopped {
		return LegacyConversionReport{}, errors.New("legacy conversion requires a stopped application snapshot")
	}
	if options.SourceSnapshotID == "" || options.BackupLocation == "" {
		return LegacyConversionReport{}, errors.New("legacy conversion snapshot and backup location are required")
	}
	if err := validateConversionBackup(options.BackupLocation, options.SourceSnapshotID); err != nil {
		return LegacyConversionReport{}, err
	}
	if options.ConverterVersion == "" {
		options.ConverterVersion = defaultLegacyConverterVersion
	}
	if options.At.IsZero() {
		options.At = time.Now().UTC()
	}
	report := LegacyConversionReport{
		ID:               "legacy-" + options.SourceSnapshotID,
		SourceSnapshotID: options.SourceSnapshotID,
		ConverterVersion: options.ConverterVersion,
		BackupLocation:   options.BackupLocation,
		ConvertedAt:      options.At.UTC(),
	}
	var existing LegacyConversionReport
	if err := s.loadPayload(
		ctx,
		"legacy_conversion_reports",
		report.ID,
		&existing,
		func() error { return nil },
	); err == nil {
		return existing, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return LegacyConversionReport{}, err
	}
	err := s.InTx(ctx, func(txCtx context.Context) error {
		var err error
		report.LegacyQueueItems, err = s.countLegacyQueueItems(txCtx)
		if err != nil {
			return err
		}
		sessions, err := listTargetPayloads[domain.TaskSession](
			txCtx,
			s,
			`SELECT payload FROM task_sessions ORDER BY id`,
			nil,
			"legacy task sessions",
			func(value domain.TaskSession) error { return value.Validate() },
		)
		if err != nil {
			return err
		}
		for _, legacySession := range sessions {
			converted, err := s.convertLegacySession(txCtx, legacySession, options.At)
			if err != nil {
				return err
			}
			if !converted.converted {
				report.SkippedSessions++
				continue
			}
			report.ConvertedSessions++
			report.ConvertedGroups++
			report.ConvertedAgents += converted.agents
			report.ConvertedExecutions += converted.executions
			report.ConvertedQueuedWork += converted.queuedWork
		}
		if err := s.VerifyTargetIntegrity(txCtx); err != nil {
			return err
		}
		report.Digest, err = legacyReportDigest(report)
		if err != nil {
			return err
		}
		payload, err := encodePayload(report)
		if err != nil {
			return err
		}
		return s.savePayload(
			txCtx,
			`INSERT INTO legacy_conversion_reports (
				id, source_snapshot_id, converter_version, backup_location, digest, created_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_snapshot_id) DO UPDATE SET
				converter_version = excluded.converter_version, backup_location = excluded.backup_location,
				digest = excluded.digest, created_at = excluded.created_at, payload = excluded.payload`,
			report.ID,
			report.SourceSnapshotID,
			report.ConverterVersion,
			report.BackupLocation,
			report.Digest,
			nullableTimeValue(report.ConvertedAt),
			payload,
		)
	})
	if err != nil {
		return LegacyConversionReport{}, err
	}
	return report, nil
}

type legacySessionConversion struct {
	converted  bool
	agents     int
	executions int
	queuedWork int
}

type backupManifestEnvelope struct {
	SnapshotID string `json:"snapshotId"`
}

func validateConversionBackup(path, snapshotID string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect legacy conversion backup: %w", err)
	}
	if !info.IsDir() {
		return errors.New("legacy conversion backup must be a directory")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read legacy conversion backup manifest: %w", err)
	}
	var manifest backupManifestEnvelope
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("decode legacy conversion backup manifest: %w", err)
	}
	if manifest.SnapshotID != snapshotID {
		return errors.New("legacy conversion backup snapshot does not match source snapshot")
	}
	return nil
}

func (s *Store) convertLegacySession(
	ctx context.Context,
	legacySession domain.TaskSession,
	at time.Time,
) (legacySessionConversion, error) {
	sessionID := domain.SessionID(legacySession.ID)
	if _, err := s.GetSession(ctx, sessionID); err == nil {
		return legacySessionConversion{}, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return legacySessionConversion{}, err
	}
	threads, err := listTargetPayloads[domain.AgentThread](
		ctx,
		s,
		`SELECT payload FROM agent_threads WHERE task_session_id = ? ORDER BY id`,
		[]any{legacySession.ID.String()},
		"legacy agent threads",
		func(value domain.AgentThread) error { return value.Validate() },
	)
	if err != nil {
		return legacySessionConversion{}, err
	}
	if len(threads) == 0 {
		return legacySessionConversion{}, fmt.Errorf("legacy session %s has no agent thread", legacySession.ID)
	}
	session, err := domain.NewSession(
		sessionID,
		legacySession.Goal,
		legacySession.WorkspaceKey,
		legacySession.CreatedAt,
	)
	if err != nil {
		return legacySessionConversion{}, err
	}
	session.UpdatedAt = legacySession.UpdatedAt
	group, err := domain.NewAgentGroup(
		domain.AgentGroupID("legacy-group-"+legacySession.ID.String()),
		session.ID,
		len(threads),
		legacySession.CreatedAt,
	)
	if err != nil {
		return legacySessionConversion{}, err
	}
	if err := s.SaveSession(ctx, session); err != nil {
		return legacySessionConversion{}, err
	}
	if err := s.SaveAgentGroup(ctx, group); err != nil {
		return legacySessionConversion{}, err
	}
	converted := legacySessionConversion{converted: true}
	for _, thread := range threads {
		agent, err := domain.NewAgent(
			domain.AgentID(thread.ID),
			session.ID,
			group.ID,
			thread.Profile,
			thread.TaskPacketID,
			thread.ContextManifestID,
			thread.GrantID,
			thread.UpdatedAt,
		)
		if err != nil {
			return legacySessionConversion{}, err
		}
		agent.RuntimeSessionRef = thread.RuntimeSessionRef
		agent.State = legacyThreadState(thread.State)
		if err := agent.Validate(); err != nil {
			return legacySessionConversion{}, err
		}
		if group.PrimaryAgentID == "" && thread.Profile == domain.ProfilePrimary {
			if err := group.SetPrimary(agent.ID, thread.UpdatedAt); err != nil {
				return legacySessionConversion{}, err
			}
		}
		if err := s.SaveAgent(ctx, agent); err != nil {
			return legacySessionConversion{}, err
		}
		converted.agents++
		runs, err := listTargetPayloads[domain.AgentRun](
			ctx,
			s,
			`SELECT payload FROM agent_runs WHERE agent_thread_id = ? ORDER BY id`,
			[]any{thread.ID.String()},
			"legacy agent runs",
			func(value domain.AgentRun) error { return value.Validate() },
		)
		if err != nil {
			return legacySessionConversion{}, err
		}
		for _, run := range runs {
			execution, err := legacyRunExecution(session, agent, run, at)
			if err != nil {
				return legacySessionConversion{}, err
			}
			if err := s.SaveAgentExecution(ctx, execution); err != nil {
				return legacySessionConversion{}, err
			}
			converted.executions++
		}
		workItems, err := s.ListByThread(ctx, thread.ID, "", 10000)
		if err != nil {
			return legacySessionConversion{}, err
		}
		for _, item := range workItems {
			if err := s.convertLegacyQueuedWork(ctx, session, agent, item, at); err != nil {
				return legacySessionConversion{}, err
			}
			converted.queuedWork++
		}
	}
	if err := s.SaveAgentGroup(ctx, group); err != nil {
		return legacySessionConversion{}, err
	}
	return converted, nil
}

func legacyRunExecution(
	session domain.Session,
	agent domain.Agent,
	run domain.AgentRun,
	fallback time.Time,
) (domain.AgentExecution, error) {
	input := domain.ExecutionInputSnapshot{
		TaskPacketID:      agent.TaskPacketID,
		ContextManifestID: agent.ContextManifestID,
		CapabilityGrantID: agent.GrantID,
		Runtime:           run.Execution,
	}
	var (
		execution domain.AgentExecution
		err       error
	)
	if run.WorkItemID != "" {
		execution, err = domain.NewQueuedWorkExecution(
			domain.AgentExecutionID(run.ID),
			session.ID,
			agent.ID,
			run.WorkItemID,
			input,
			run.StartedAt,
		)
	} else {
		execution, err = domain.NewAgentExecution(
			domain.AgentExecutionID(run.ID),
			session.ID,
			agent.ID,
			domain.RequestID("legacy:"+run.ID.String()),
			domain.ExecutionResume,
			"",
			input,
			run.StartedAt,
		)
	}
	if err != nil {
		return domain.AgentExecution{}, err
	}
	if err := execution.MarkRunning(run.StartedAt); err != nil {
		return domain.AgentExecution{}, err
	}
	settledAt := run.SettledAt
	if settledAt.IsZero() {
		settledAt = fallback.UTC()
	}
	if settledAt.Before(run.StartedAt) {
		return domain.AgentExecution{}, errors.New("legacy run settlement precedes its start")
	}
	if err := execution.BeginSettlement(settledAt); err != nil {
		return domain.AgentExecution{}, err
	}
	outcome := legacyRunOutcome(run)
	failureCode := domain.ExecutionFailureFromLegacy(run.FailureCode)
	if outcome == domain.ExecutionFailed && failureCode == "" {
		failureCode = domain.ExecutionFailureLegacy
	}
	if err := execution.Settle(outcome, failureCode, settledAt); err != nil {
		return domain.AgentExecution{}, err
	}
	return execution, nil
}

func (s *Store) convertLegacyQueuedWork(
	ctx context.Context,
	session domain.Session,
	agent domain.Agent,
	legacy domain.QueuedWorkItem,
	fallback time.Time,
) error {
	if _, err := s.GetQueuedWork(ctx, legacy.ID); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if legacy.TaskSessionID != domain.TaskSessionID(session.ID) ||
		legacy.AgentThreadID != domain.AgentThreadID(agent.ID) {
		return errors.New("legacy queued work ownership does not match converted agent")
	}
	work, err := domain.NewQueuedWork(
		legacy.ID,
		session.ID,
		agent.ID,
		legacy.Sequence,
		legacy.Prompt,
		domain.ExecutionInputSnapshot{
			TaskPacketID:      legacy.TaskPacketID,
			ContextManifestID: legacy.ContextManifestID,
			CapabilityGrantID: legacy.CapabilityGrantID,
			Runtime:           legacy.Execution,
		},
		legacy.CreatedAt,
	)
	if err != nil {
		return err
	}
	switch legacy.Status {
	case domain.WorkItemQueued:
	case domain.WorkItemCancelled:
		work.Status = domain.QueuedWorkCancelled
		work.FinishedAt = legacy.FinishedAt
	case domain.WorkItemRunning:
		work.Status = domain.QueuedWorkInterrupted
		work.ExecutionID = domain.AgentExecutionID(legacy.AgentRunID)
		work.StartedAt = legacy.StartedAt
		work.FinishedAt = fallback.UTC()
		work.FailureCode = domain.ExecutionFailureLegacy
	case domain.WorkItemPaused:
		work.Status = domain.QueuedWorkPaused
		work.ExecutionID = domain.AgentExecutionID(legacy.AgentRunID)
		work.StartedAt = legacy.StartedAt
		work.FinishedAt = legacy.FinishedAt
		work.FailureCode = domain.ExecutionFailureFromLegacy(legacy.FailureCode)
	case domain.WorkItemFailed:
		work.Status = domain.QueuedWorkFailed
		work.ExecutionID = domain.AgentExecutionID(legacy.AgentRunID)
		work.StartedAt = legacy.StartedAt
		work.FinishedAt = legacy.FinishedAt
		work.FailureCode = domain.ExecutionFailureFromLegacy(legacy.FailureCode)
		if work.FailureCode == "" {
			work.FailureCode = domain.ExecutionFailureLegacy
		}
	case domain.WorkItemInterrupted:
		work.Status = domain.QueuedWorkInterrupted
		work.ExecutionID = domain.AgentExecutionID(legacy.AgentRunID)
		work.StartedAt = legacy.StartedAt
		work.FinishedAt = legacy.FinishedAt
		work.FailureCode = domain.ExecutionFailureFromLegacy(legacy.FailureCode)
	case domain.WorkItemCompleted:
		work.Status = domain.QueuedWorkCompleted
		work.ExecutionID = domain.AgentExecutionID(legacy.AgentRunID)
		work.StartedAt = legacy.StartedAt
		work.FinishedAt = legacy.FinishedAt
		work.FailureCode = domain.ExecutionFailureFromLegacy(legacy.FailureCode)
	default:
		return errors.New("legacy queued work has an unknown status")
	}
	if work.ExecutionID != "" {
		execution, err := s.GetAgentExecution(ctx, work.ExecutionID)
		if err != nil {
			return fmt.Errorf("load execution for legacy queued work %s: %w", work.ID, err)
		}
		if execution.AgentID != work.AgentID || execution.WorkItemID != work.ID {
			return errors.New("legacy queued work execution ownership does not match converted work")
		}
	}
	if err := work.Validate(); err != nil {
		return err
	}
	if err := s.SaveQueuedWork(ctx, work); err != nil {
		return err
	}
	return nil
}

func legacyThreadState(state domain.AgentThreadState) domain.AgentState {
	switch state {
	case domain.ThreadIdle:
		return domain.AgentIdle
	case domain.ThreadPaused:
		return domain.AgentPaused
	case domain.ThreadFailed:
		return domain.AgentFailed
	case domain.ThreadClosed:
		return domain.AgentClosed
	default:
		return domain.AgentInterrupted
	}
}

func legacyRunOutcome(run domain.AgentRun) domain.ExecutionOutcome {
	if run.SettledAt.IsZero() {
		return domain.ExecutionInterrupted
	}
	switch run.Outcome {
	case domain.RunCompleted:
		return domain.ExecutionCompleted
	case domain.RunPaused:
		return domain.ExecutionPaused
	case domain.RunFailed:
		return domain.ExecutionFailed
	default:
		return domain.ExecutionInterrupted
	}
}

func (s *Store) countLegacyQueueItems(ctx context.Context) (int, error) {
	var count int
	if err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM work_queue_items`,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count legacy work queue items: %w", err)
	}
	return count, nil
}

func legacyReportDigest(report LegacyConversionReport) (string, error) {
	report.Digest = ""
	encoded, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("encode legacy conversion report: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
