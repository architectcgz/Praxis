package jsonl

import (
	"errors"
	agentmodel "praxis/internal/agent"
	"praxis/internal/fault"
	modelapi "praxis/internal/model"
	modelconfig "praxis/internal/model/config"
	projectmodel "praxis/internal/project"
	sessionmodel "praxis/internal/session"
	taskmodel "praxis/internal/task"
	"praxis/internal/utils/pathutil"
	"praxis/internal/workflow"
	workspacemodel "praxis/internal/workspace"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// 以下校验仅用于磁盘输入，拒绝非法字段，不清洗或修复持久化数据。
func validateProject(p projectmodel.Project) error {
	if p.ID == "" || p.ID.String() != strings.TrimSpace(p.ID.String()) {
		return fault.InvalidValue("project.id", "id is required")
	}
	if p.Name == "" || strings.ContainsAny(p.Name, "\x00\r\n") {
		return fault.InvalidValue("project.name", "name is required and cannot contain control characters")
	}
	if !pathutil.IsAbsoluteNormalized(p.Path) {
		return fault.InvalidValue("project.path", "path must be an absolute normalized path")
	}
	if p.DefaultWorkspaceID == "" || p.DefaultWorkspaceID.String() != strings.TrimSpace(p.DefaultWorkspaceID.String()) {
		return fault.InvalidValue("project.defaultWorkspaceID", "default workspace id is required")
	}
	if p.State != projectmodel.ProjectActive && p.State != projectmodel.ProjectArchived {
		return fault.InvalidValue("project.state", "unknown project state")
	}
	if p.ApprovalMode != nil && !p.ApprovalMode.ValidDefault() {
		return fault.InvalidValue("project.approvalMode", "default approval mode must be ask, auto or yolo")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return fault.InvalidValue("project.timestamps", "timestamps are invalid")
	}
	return nil
}

func validateWorkspace(w workspacemodel.Workspace) error {
	for _, id := range []string{w.ID.String(), w.ProjectID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("workspace", "required reference must be canonical and nonempty")
		}
	}
	if w.Kind != workspacemodel.WorkspaceProjectRoot && w.Kind != workspacemodel.WorkspaceWorktree && w.Kind != workspacemodel.WorkspaceTemporary {
		return fault.InvalidValue("workspace.kind", "unknown workspace kind")
	}
	if !pathutil.IsAbsoluteNormalized(w.Path) {
		return fault.InvalidValue("workspace.path", "path must be an absolute normalized path")
	}
	if w.State != workspacemodel.WorkspaceReady && w.State != workspacemodel.WorkspaceUnavailable && w.State != workspacemodel.WorkspaceArchived {
		return fault.InvalidValue("workspace.state", "unknown workspace state")
	}
	if w.Revision == 0 {
		return fault.InvalidValue("workspace.revision", "revision must be positive")
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() || w.UpdatedAt.Before(w.CreatedAt) {
		return fault.InvalidValue("workspace.timestamps", "timestamps are invalid")
	}
	return nil
}

func validateAgent(a agentmodel.Agent) error {
	for _, id := range []string{a.ID.String(), a.SessionID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("agent", "required reference must be canonical and nonempty")
		}
	}
	if !definitionIDPattern.MatchString(a.DefinitionID.String()) {
		return fault.InvalidValue("agent.definitionID", "invalid agent definition identity")
	}
	switch a.Profile {
	case agentmodel.ProfilePrimary, agentmodel.ProfileDelegate, agentmodel.ProfileAdvisor, agentmodel.ProfileCurator:
	default:
		return fault.InvalidValue("agent.profile", "unknown agent profile")
	}
	switch a.State {
	case agentmodel.AgentIdle, agentmodel.AgentExecuting, agentmodel.AgentWaiting, agentmodel.AgentPausing, agentmodel.AgentPaused, agentmodel.AgentInterrupted, agentmodel.AgentFailed:
	default:
		return fault.InvalidValue("agent.state", "unknown agent state")
	}
	if a.State == agentmodel.AgentExecuting || a.State == agentmodel.AgentPausing {
		if a.CurrentTaskID == "" || a.CurrentTaskID.String() != strings.TrimSpace(a.CurrentTaskID.String()) {
			return fault.InvalidValue("agent.currentTaskID", "active state requires a task")
		}
	} else if a.CurrentTaskID != "" {
		return fault.InvalidValue("agent.currentTaskID", "inactive state cannot retain a task")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return fault.InvalidValue("agent.timestamps", "timestamps are invalid")
	}
	return nil
}

func validateWaitCondition(w workflow.WaitCondition) error {
	for _, id := range []string{w.ID.String(), w.AgentID.String(), w.TaskID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("waitCondition", "required reference must be canonical and nonempty")
		}
	}
	if !slices.Contains([]workflow.WaitConditionKind{workflow.WaitForDelivery, workflow.WaitForApproval, workflow.WaitForChild}, w.Kind) ||
		w.Mode != workflow.WaitAny && w.Mode != workflow.WaitAll ||
		!slices.Contains([]workflow.WaitConditionStatus{workflow.WaitPending, workflow.WaitResolved, workflow.WaitCancelled}, w.Status) {
		return fault.InvalidValue("waitCondition", "unknown kind, mode or status")
	}
	if len(w.TargetIDs) == 0 {
		return fault.InvalidValue("waitCondition.targetIDs", "at least one target is required")
	}
	seenTargets := make(map[string]bool, len(w.TargetIDs))
	for _, id := range w.TargetIDs {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") || seenTargets[id] {
			return fault.InvalidValue("waitCondition.targetIDs", "target is invalid or duplicated")
		}
		seenTargets[id] = true
	}
	seenResolved := make(map[string]bool, len(w.ResolvedTargetIDs))
	for _, id := range w.ResolvedTargetIDs {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") || !seenTargets[id] || seenResolved[id] {
			return fault.InvalidValue("waitCondition.resolvedTargetIDs", "resolved target is invalid, unknown or duplicated")
		}
		seenResolved[id] = true
	}
	if w.CreatedAt.IsZero() || !w.ResolvedAt.IsZero() && w.ResolvedAt.Before(w.CreatedAt) {
		return fault.InvalidValue("waitCondition.timestamps", "timestamps are invalid")
	}
	if (w.Status == workflow.WaitPending) != w.ResolvedAt.IsZero() {
		return fault.InvalidValue("waitCondition.resolvedAt", "resolution time does not match status")
	}
	if w.Status == workflow.WaitResolved && (w.Mode == workflow.WaitAny && len(w.ResolvedTargetIDs) == 0 || w.Mode == workflow.WaitAll && len(w.ResolvedTargetIDs) != len(w.TargetIDs)) {
		return fault.InvalidValue("waitCondition.resolvedTargetIDs", "resolved targets do not satisfy the wait")
	}
	return nil
}

func validateUsageRecord(r modelapi.ModelUsageRecord) error {
	for _, id := range []string{
		r.SessionID,
		r.AgentID,
		r.TaskID,
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
	for _, id := range []string{r.ID.String(), r.AgentID.String(), r.TargetTaskID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("agentControlCommand", "required reference must be canonical and nonempty")
		}
	}
	if r.Kind != agentmodel.AgentControlPause && r.Kind != agentmodel.AgentControlCancel {
		return fault.InvalidValue("agentControlCommand.kind", "unknown control kind")
	}
	if r.Status != agentmodel.AgentControlPending && r.Status != agentmodel.AgentControlApplied {
		return fault.InvalidValue("agentControlCommand.status", "unknown control status")
	}
	if r.CreatedAt.IsZero() || !r.AppliedAt.IsZero() && r.AppliedAt.Before(r.CreatedAt) {
		return fault.InvalidValue("agentControlCommand.timestamps", "timestamps are invalid")
	}
	if (r.Status == agentmodel.AgentControlPending) != r.AppliedAt.IsZero() {
		return fault.InvalidValue("agentControlCommand.appliedAt", "apply time does not match status")
	}
	return nil
}

func validateTask(e taskmodel.Task) error {
	if e.Progress.OutputBytes < 0 || e.Progress.ToolCalls < 0 || e.Progress.ToolCalls > taskmodel.MaxToolCalls || e.Progress.Iterations > taskmodel.MaxIterations || e.Progress.Iterations > e.Progress.ModelRequests || e.Progress.ModelRequests > 0 && e.Progress.NextSequence != e.Progress.ModelRequests+1 {
		return fault.InvalidValue("task.progress", "usage counters or request sequence are invalid")
	}
	if (e.Progress.FinalAssistantID == "") != (e.Progress.FinalStopReason == "") || e.Progress.FinalStopReason != "" && e.Progress.FinalStopReason != "completed" && e.Progress.FinalStopReason != "stop_sequence" {
		return fault.InvalidValue("task.progress", "final output evidence is inconsistent")
	}
	for _, id := range []string{
		e.ID.String(),
		e.SessionID.String(),
		e.AgentID.String(),
		e.RequestID.String(),
		e.InputMessageID,
	} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("task", "required reference must be canonical and nonempty")
		}
	}
	switch e.Status {
	case taskmodel.TaskPending, taskmodel.TaskStarting, taskmodel.TaskRunning, taskmodel.TaskEnding, taskmodel.TaskEnded:
	default:
		return fault.InvalidValue("task", "unknown status")
	}
	if (e.ProviderID == "") != (e.ModelID == "") || e.ProviderID == "" && e.ReasoningLevel != "" {
		return fault.InvalidValue("task.model", "provider and model are required together")
	}
	for _, value := range []string{e.ProviderID, e.ModelID, e.ReasoningLevel} {
		if value != strings.TrimSpace(value) {
			return fault.InvalidValue("task.model", "model selection must be canonical")
		}
	}
	if e.Status == taskmodel.TaskPending {
		if e.Sequence == 0 || !reflect.ValueOf(e.Config).IsZero() || !reflect.ValueOf(e.ContextSelection).IsZero() || e.Progress != (taskmodel.Progress{}) || !e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "" || e.FailureCode != "" {
			return fault.InvalidValue("task.pending", "pending task requires a sequence and cannot contain execution state")
		}
	} else if err := validateTaskConfig(e.Config); err != nil {
		return fault.FieldError("task.config", err)
	}
	if slices.Contains(e.ContextSelection.ExcludedMessageIDs, e.InputMessageID) {
		return fault.InvalidValue("task.contextSelection", "current input cannot be excluded")
	}
	for _, id := range e.ContextSelection.ExcludedMessageIDs {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("task.contextSelection.excludedMessageIDs", "message id must be canonical and nonempty")
		}
	}
	for _, ref := range e.ContextSelection.SessionContextRefs {
		if ref.ID == "" || ref.ID != strings.TrimSpace(ref.ID) || ref.Digest == "" || ref.Digest != strings.TrimSpace(ref.Digest) {
			return fault.InvalidValue("task.contextSelection.sessionContextRefs", "context reference must be canonical and nonempty")
		}
	}
	if e.CreatedAt.IsZero() || !e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt) || !e.EndedAt.IsZero() && e.EndedAt.Before(e.CreatedAt) {
		return fault.InvalidValue("task.timestamps", "timestamps are invalid")
	}
	if e.Status == taskmodel.TaskStarting && (!e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fault.InvalidValue("task", "starting task has terminal fields")
	}
	if (e.Status == taskmodel.TaskRunning || e.Status == taskmodel.TaskEnding) && (e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fault.InvalidValue("task", "active task has invalid lifecycle fields")
	}
	if e.Status == taskmodel.TaskEnded && (e.StartedAt.IsZero() || e.EndedAt.IsZero() || !taskmodel.ValidTaskOutcome(e.Outcome) || e.Outcome == taskmodel.TaskFailed && e.FailureCode == "") {
		return fault.InvalidValue("task", "ended task has invalid terminal fields")
	}
	if !taskmodel.ValidFailureCode(e.FailureCode) || e.FailureCode == taskmodel.TaskFailureRequestCanceled && e.Outcome != taskmodel.TaskPaused && e.Outcome != taskmodel.TaskInterrupted {
		return fault.InvalidValue("task.failureCode", "failure code does not match outcome")
	}
	if len([]rune(e.FailureMessage)) > taskmodel.MaxTaskFailureMessageRunes || e.FailureMessage != "" && (e.Status != taskmodel.TaskEnded || e.Outcome != taskmodel.TaskFailed) {
		return fault.InvalidValue("task.failureMessage", "failure message is invalid")
	}
	return nil
}

func validateTaskConfig(s taskmodel.Config) error {
	if !definitionIDPattern.MatchString(s.AgentDefinitionID.String()) {
		return fault.InvalidValue("task.config.agentDefinition", "definition id is required")
	}
	if err := validateTaskModel(s.Model); err != nil {
		return fault.FieldError("task.config.model", err)
	}
	if !pathutil.IsAbsoluteNormalized(s.WorkspacePath) {
		return fault.InvalidValue("task.config.workspacePath", "workspace path must be an absolute normalized path")
	}
	return nil
}

func validateTaskModel(s modelapi.Config) error {
	seen := map[string]bool{}
	for _, level := range s.ReasoningLevels {
		if level == "" || level != strings.TrimSpace(level) || seen[level] {
			return errors.New("task model reasoning capabilities must be canonical and unique")
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
			return errors.New("task model has an unsupported Anthropic effort level")
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
		return errors.New("task reasoning level is not supported by frozen capabilities")
	}
	if s.ProviderID == "" || s.ModelID == "" || s.ProviderID != strings.TrimSpace(s.ProviderID) || s.ModelID != strings.TrimSpace(s.ModelID) || s.ReasoningLevel != strings.TrimSpace(s.ReasoningLevel) {
		return errors.New("task model identity fields must be canonical and nonempty")
	}
	switch s.APIFormat {
	case string(modelconfig.APIFormatAnthropicMessages), string(modelconfig.APIFormatOpenAIResponses), string(modelconfig.APIFormatOpenAIChatCompletions):
	default:
		return errors.New("task model API format is invalid")
	}
	if s.ContextWindow <= 0 || s.MaxOutputTokens <= 0 || s.MaxOutputTokens >= s.ContextWindow {
		return errors.New("task model capability limits are invalid")
	}
	if _, err := modelconfig.ValidateModelBaseURL(s.BaseURL); err != nil {
		return err
	}
	_, err := modelconfig.ValidateModelProxyURL(s.ProxyURL)
	return err
}

func validateContextEntry(e sessionmodel.SessionContextEntry) error {
	if e.SourceTaskID.String() != strings.TrimSpace(e.SourceTaskID.String()) {
		return fault.InvalidValue("sessionContext.sourceTaskID", "source task id must be canonical")
	}
	for _, id := range []string{e.ID.String(), e.SessionID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("sessionContext", "entry id and session id must be canonical and nonempty")
		}
	}
	switch e.Kind {
	case sessionmodel.SessionContextUserMessage, sessionmodel.SessionContextAcceptedConclusion, sessionmodel.SessionContextDecision, sessionmodel.SessionContextReference:
	default:
		return fault.InvalidValue("sessionContext.kind", "unknown context entry kind")
	}
	if e.Content == "" || e.Content != strings.TrimSpace(e.Content) || !utf8.ValidString(e.Content) || len(e.Content) > sessionmodel.MaxSessionContextContentBytes {
		return fault.InvalidValue("sessionContext.content", "content must be canonical UTF-8 text within the size limit")
	}
	if e.ContentDigest != sessionmodel.ContentDigest(e.Content) {
		return fault.InvalidValue("sessionContext.contentDigest", "content digest does not match content")
	}
	if e.CreatedAt.IsZero() {
		return fault.InvalidValue("sessionContext.createdAt", "createdAt is required")
	}
	return nil
}
