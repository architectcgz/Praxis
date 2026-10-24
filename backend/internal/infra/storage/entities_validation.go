package storage

import (
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/utils/pathutil"
	"regexp"
	"slices"
	"strings"
)

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// 以下校验仅用于磁盘输入，拒绝非法字段，不清洗或修复持久化数据。
func validateProject(p projectmodel.Project) error {
	if p.ID == "" || p.ID.String() != strings.TrimSpace(p.ID.String()) {
		return fmt.Errorf("project.id: id is required: %w", errorcode.ErrInvalidValue)
	}
	if p.Name == "" || strings.ContainsAny(p.Name, "\x00\r\n") {
		return fmt.Errorf("project.name: name is required and cannot contain control characters: %w", errorcode.ErrInvalidValue)
	}
	if !pathutil.IsAbsoluteNormalized(p.Path) {
		return fmt.Errorf("project.path: path must be an absolute normalized path: %w", errorcode.ErrInvalidValue)
	}
	if p.DefaultWorkspaceID == "" || p.DefaultWorkspaceID.String() != strings.TrimSpace(p.DefaultWorkspaceID.String()) {
		return fmt.Errorf("project.defaultWorkspaceID: default workspace id is required: %w", errorcode.ErrInvalidValue)
	}
	if p.State != projectmodel.ProjectActive && p.State != projectmodel.ProjectArchived {
		return fmt.Errorf("project.state: unknown project state: %w", errorcode.ErrInvalidValue)
	}
	if p.ApprovalMode != nil && !p.ApprovalMode.ValidDefault() {
		return fmt.Errorf("project.approvalMode: default approval mode must be ask, auto or yolo: %w", errorcode.ErrInvalidValue)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return fmt.Errorf("project.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

func validateWorkspace(w workspacemodel.Workspace) error {
	for _, id := range []string{w.ID.String(), w.ProjectID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("workspace: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if w.Kind != workspacemodel.WorkspaceProjectRoot && w.Kind != workspacemodel.WorkspaceWorktree && w.Kind != workspacemodel.WorkspaceTemporary {
		return fmt.Errorf("workspace.kind: unknown workspace kind: %w", errorcode.ErrInvalidValue)
	}
	if !pathutil.IsAbsoluteNormalized(w.Path) {
		return fmt.Errorf("workspace.path: path must be an absolute normalized path: %w", errorcode.ErrInvalidValue)
	}
	if w.State != workspacemodel.WorkspaceReady && w.State != workspacemodel.WorkspaceUnavailable && w.State != workspacemodel.WorkspaceArchived {
		return fmt.Errorf("workspace.state: unknown workspace state: %w", errorcode.ErrInvalidValue)
	}
	if w.Revision == 0 {
		return fmt.Errorf("workspace.revision: revision must be positive: %w", errorcode.ErrInvalidValue)
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() || w.UpdatedAt.Before(w.CreatedAt) {
		return fmt.Errorf("workspace.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

func validateAgent(a agentmodel.Agent) error {
	for _, id := range []string{a.ID.String(), a.SessionID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("agent: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if !definitionIDPattern.MatchString(a.DefinitionID.String()) {
		return fmt.Errorf("agent.definitionID: invalid agent definition identity: %w", errorcode.ErrInvalidValue)
	}
	if a.Kind != agentmodel.KindMain && a.Kind != agentmodel.KindSubagent {
		return fmt.Errorf("agent.kind: unknown agent kind: %w", errorcode.ErrInvalidValue)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return fmt.Errorf("agent.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

func validateWaitCondition(w workflow.WaitCondition) error {
	for _, id := range []string{w.ID.String(), w.AgentID.String(), w.ExecutionID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("waitCondition: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if !slices.Contains([]workflow.WaitConditionKind{workflow.WaitForDelivery, workflow.WaitForApproval, workflow.WaitForChild}, w.Kind) ||
		w.Mode != workflow.WaitAny && w.Mode != workflow.WaitAll ||
		!slices.Contains([]workflow.WaitConditionStatus{workflow.WaitPending, workflow.WaitResolved, workflow.WaitCancelled}, w.Status) {
		return fmt.Errorf("waitCondition: unknown kind, mode or status: %w", errorcode.ErrInvalidValue)
	}
	if len(w.TargetIDs) == 0 {
		return fmt.Errorf("waitCondition.targetIDs: at least one target is required: %w", errorcode.ErrInvalidValue)
	}
	seenTargets := make(map[string]bool, len(w.TargetIDs))
	for _, id := range w.TargetIDs {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") || seenTargets[id] {
			return fmt.Errorf("waitCondition.targetIDs: target is invalid or duplicated: %w", errorcode.ErrInvalidValue)
		}
		seenTargets[id] = true
	}
	seenResolved := make(map[string]bool, len(w.ResolvedTargetIDs))
	for _, id := range w.ResolvedTargetIDs {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") || !seenTargets[id] || seenResolved[id] {
			return fmt.Errorf("waitCondition.resolvedTargetIDs: resolved target is invalid, unknown or duplicated: %w", errorcode.ErrInvalidValue)
		}
		seenResolved[id] = true
	}
	if w.CreatedAt.IsZero() || !w.ResolvedAt.IsZero() && w.ResolvedAt.Before(w.CreatedAt) {
		return fmt.Errorf("waitCondition.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	if (w.Status == workflow.WaitPending) != w.ResolvedAt.IsZero() {
		return fmt.Errorf("waitCondition.resolvedAt: resolution time does not match status: %w", errorcode.ErrInvalidValue)
	}
	if w.Status == workflow.WaitResolved && (w.Mode == workflow.WaitAny && len(w.ResolvedTargetIDs) == 0 || w.Mode == workflow.WaitAll && len(w.ResolvedTargetIDs) != len(w.TargetIDs)) {
		return fmt.Errorf("waitCondition.resolvedTargetIDs: resolved targets do not satisfy the wait: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

func validateUsageRecord(r providerapi.UsageRecord) error {
	for _, id := range []string{
		r.SessionID,
		r.AgentID,
		r.ExecutionID,
		r.TurnID.String(),
	} {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") {
			return errors.New("token usage identity must be a non-empty canonical string")
		}
	}
	if !r.Usage.Valid() {
		return errors.New("invalid token usage counts")
	}
	return nil
}

func validateControlCommand(r agentmodel.AgentControlCommand) error {
	for _, id := range []string{r.ID.String(), r.AgentID.String(), r.TargetExecutionID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("agentControlCommand: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if r.Kind != agentmodel.AgentControlPause && r.Kind != agentmodel.AgentControlCancel {
		return fmt.Errorf("agentControlCommand.kind: unknown control kind: %w", errorcode.ErrInvalidValue)
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("agentControlCommand.createdAt: creation time is required: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

// validateAgentExecution 仅在 JSONL 恢复时校验解码后的执行记录，不修复或补默认值。
func validateAgentExecution(e agentexecution.Execution) error {
	if e.Progress.ModelOutputBytes < 0 || e.Progress.Iterations > e.Progress.ModelRequests || e.Progress.ModelRequests > 0 && e.Progress.NextSequence != e.Progress.ModelRequests+1 {
		return fmt.Errorf("execution.progress: usage counters or request sequence are invalid: %w", errorcode.ErrInvalidValue)
	}
	for _, id := range []string{
		e.ID.String(),
		e.SessionID.String(),
		e.AgentID.String(),
		e.RequestID.String(),
		e.InputEntryID,
	} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("agent_execution: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	switch e.Status {
	case agentexecution.ExecutionStarting, agentexecution.ExecutionRunning, agentexecution.ExecutionEnding, agentexecution.ExecutionEnded:
	default:
		return fmt.Errorf("agent_execution: unknown status: %w", errorcode.ErrInvalidValue)
	}
	if err := validateExecutionConfig(e.Config); err != nil {
		return fmt.Errorf("execution.config: %w", err)
	}

	if e.CreatedAt.IsZero() || !e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt) || !e.EndedAt.IsZero() && e.EndedAt.Before(e.CreatedAt) {
		return fmt.Errorf("execution.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	if e.Status == agentexecution.ExecutionStarting && (!e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fmt.Errorf("agent_execution: starting execution has terminal fields: %w", errorcode.ErrInvalidValue)
	}
	if (e.Status == agentexecution.ExecutionRunning || e.Status == agentexecution.ExecutionEnding) && (e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fmt.Errorf("agent_execution: active execution has invalid lifecycle fields: %w", errorcode.ErrInvalidValue)
	}
	if e.Status == agentexecution.ExecutionEnded {
		if e.StartedAt.IsZero() || e.EndedAt.IsZero() || e.Outcome == agentexecution.ExecutionFailed && e.FailureCode == "" {
			return fmt.Errorf("agent_execution: ended execution has invalid terminal fields: %w", errorcode.ErrInvalidValue)
		}
		switch e.Outcome {
		case agentexecution.ExecutionCompleted, agentexecution.ExecutionYielded, agentexecution.ExecutionPaused,
			agentexecution.ExecutionFailed, agentexecution.ExecutionInterrupted:
		default:
			return fmt.Errorf("agent_execution: ended execution has invalid terminal fields: %w", errorcode.ErrInvalidValue)
		}
	}
	if e.FailureCode == agentexecution.ExecutionFailureRequestCanceled && e.Outcome != agentexecution.ExecutionPaused && e.Outcome != agentexecution.ExecutionInterrupted {
		return fmt.Errorf("execution.failureCode: failure code does not match outcome: %w", errorcode.ErrInvalidValue)
	}
	if len([]rune(e.FailureMessage)) > agentexecution.MaxExecutionFailureMessageRunes || e.FailureMessage != "" && (e.Status != agentexecution.ExecutionEnded || e.Outcome != agentexecution.ExecutionFailed) {
		return fmt.Errorf("execution.failureMessage: failure message is invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

func validateExecutionConfig(s agentexecution.ExecutionConfig) error {
	if !definitionIDPattern.MatchString(s.AgentDefinitionID.String()) {
		return fmt.Errorf("execution.config.agentDefinition: definition id is required: %w", errorcode.ErrInvalidValue)
	}
	if err := validateExecutionModel(s.Model); err != nil {
		return fmt.Errorf("execution.config.model: %w: %w", errorcode.ErrInvalidValue, err)
	}
	return nil
}

func validateExecutionModel(s modelapi.Config) error {
	seen := map[string]bool{}
	for _, level := range s.ReasoningLevels {
		if level == "" || level != strings.TrimSpace(level) || seen[level] {
			return errors.New("execution model reasoning capabilities must be canonical and unique")
		}
		seen[level] = true
		if s.APIFormat == string(modelconfig.APIFormatAnthropicMessages) && (!slices.Contains([]string{
			"off",
			"low",
			"medium",
			"high",
			"xhigh",
			"max",
		}, level) || level == "off" && !s.Thinking.SupportsDisabled) {
			return errors.New("execution model has an unsupported Anthropic effort level")
		}
	}
	switch s.Thinking.Mode {
	case "", "adaptive":
		if s.Thinking.BudgetTokens != 0 {
			return errors.New("thinking budget is only supported in enabled mode")
		}
	case "enabled":
		if s.Thinking.BudgetTokens < 1024 || s.Thinking.BudgetTokens >= s.MaxOutputTokens {
			return errors.New("thinking budget must be at least 1024 and less than max output tokens")
		}
	default:
		return errors.New("unsupported or non-canonical thinking mode")
	}
	if s.APIFormat != string(modelconfig.APIFormatAnthropicMessages) && s.Thinking != (modelconfig.ModelThinking{}) {
		return errors.New("thinking configuration requires anthropic_messages")
	}
	if s.ReasoningLevel != "" && !slices.Contains(s.ReasoningLevels, s.ReasoningLevel) {
		return errors.New("execution reasoning level is not supported by frozen capabilities")
	}
	if s.ProviderID == "" || s.ModelID == "" || s.ProviderID != strings.TrimSpace(s.ProviderID) || s.ModelID != strings.TrimSpace(s.ModelID) || s.ReasoningLevel != strings.TrimSpace(s.ReasoningLevel) {
		return errors.New("execution model identity fields must be canonical and nonempty")
	}
	switch s.APIFormat {
	case string(modelconfig.APIFormatAnthropicMessages), string(modelconfig.APIFormatOpenAIResponses), string(modelconfig.APIFormatOpenAIChatCompletions):
	default:
		return errors.New("execution model API format is invalid")
	}
	if s.ContextWindow <= 0 || s.MaxOutputTokens <= 0 || s.MaxOutputTokens >= s.ContextWindow {
		return errors.New("execution model capability limits are invalid")
	}
	if _, err := modelconfig.ValidateModelBaseURL(s.BaseURL); err != nil {
		return err
	}
	_, err := modelconfig.ValidateModelProxyURL(s.ProxyURL)
	return err
}
