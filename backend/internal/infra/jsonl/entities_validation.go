package jsonl

import (
	"errors"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/fault"
	agentmodel "praxis/internal/service/agent"
	projectmodel "praxis/internal/service/project"
	sessionmodel "praxis/internal/service/session"

	"praxis/internal/utils/pathutil"
	"praxis/internal/workflow"
	workspacemodel "praxis/internal/workspace"
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
		if a.CurrentExecutionID == "" || a.CurrentExecutionID.String() != strings.TrimSpace(a.CurrentExecutionID.String()) {
			return fault.InvalidValue("agent.currentExecutionID", "active state requires a task")
		}
	} else if a.CurrentExecutionID != "" {
		return fault.InvalidValue("agent.currentExecutionID", "inactive state cannot retain a task")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return fault.InvalidValue("agent.timestamps", "timestamps are invalid")
	}
	return nil
}

func validateWaitCondition(w workflow.WaitCondition) error {
	for _, id := range []string{w.ID.String(), w.AgentID.String(), w.ExecutionID.String()} {
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

func validateAgentExecution(e agentmodel.Execution) error {
	if e.Progress.OutputBytes < 0 || e.Progress.ToolCalls < 0 || e.Progress.ToolCalls > agentmodel.MaxToolCalls || e.Progress.Iterations > agentmodel.MaxIterations || e.Progress.Iterations > e.Progress.ModelRequests || e.Progress.ModelRequests > 0 && e.Progress.NextSequence != e.Progress.ModelRequests+1 {
		return fault.InvalidValue("execution.progress", "usage counters or request sequence are invalid")
	}
	if (e.Progress.FinalAssistantID == "") != (e.Progress.FinalStopReason == "") || e.Progress.FinalStopReason != "" && e.Progress.FinalStopReason != "completed" && e.Progress.FinalStopReason != "stop_sequence" {
		return fault.InvalidValue("execution.progress", "final output evidence is inconsistent")
	}
	for _, id := range []string{
		e.ID.String(),
		e.SessionID.String(),
		e.AgentID.String(),
		e.RequestID.String(),
		e.InputMessageID,
	} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("agent_execution", "required reference must be canonical and nonempty")
		}
	}
	switch e.Status {
	case agentmodel.ExecutionStarting, agentmodel.ExecutionRunning, agentmodel.ExecutionWaitingApproval, agentmodel.ExecutionEnding, agentmodel.ExecutionEnded:
	default:
		return fault.InvalidValue("agent_execution", "unknown status")
	}
	if (e.ProviderID == "") != (e.ModelID == "") || e.ProviderID == "" && e.ReasoningLevel != "" {
		return fault.InvalidValue("execution.model", "provider and model are required together")
	}
	for _, value := range []string{e.ProviderID, e.ModelID, e.ReasoningLevel} {
		if value != strings.TrimSpace(value) {
			return fault.InvalidValue("execution.model", "model selection must be canonical")
		}
	}
	if err := validateExecutionConfig(e.Config); err != nil {
		return fault.FieldError("execution.config", err)
	}

	if slices.Contains(e.ContextSelection.ExcludedMessageIDs, e.InputMessageID) {
		return fault.InvalidValue("execution.contextSelection", "current input cannot be excluded")
	}
	for _, id := range e.ContextSelection.ExcludedMessageIDs {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("execution.contextSelection.excludedMessageIDs", "message id must be canonical and nonempty")
		}
	}
	for _, ref := range e.ContextSelection.SessionContextRefs {
		if ref.ID == "" || ref.ID != strings.TrimSpace(ref.ID) || ref.Digest == "" || ref.Digest != strings.TrimSpace(ref.Digest) {
			return fault.InvalidValue("execution.contextSelection.sessionContextRefs", "context reference must be canonical and nonempty")
		}
	}
	if e.CreatedAt.IsZero() || !e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt) || !e.EndedAt.IsZero() && e.EndedAt.Before(e.CreatedAt) {
		return fault.InvalidValue("execution.timestamps", "timestamps are invalid")
	}
	if e.Status == agentmodel.ExecutionStarting && (!e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fault.InvalidValue("agent_execution", "starting execution has terminal fields")
	}
	if (e.Status == agentmodel.ExecutionRunning || e.Status == agentmodel.ExecutionWaitingApproval || e.Status == agentmodel.ExecutionEnding) && (e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fault.InvalidValue("agent_execution", "active execution has invalid lifecycle fields")
	}
	if e.Status == agentmodel.ExecutionEnded && (e.StartedAt.IsZero() || e.EndedAt.IsZero() || !agentmodel.ValidExecutionOutcome(e.Outcome) || e.Outcome == agentmodel.ExecutionFailed && e.FailureCode == "") {
		return fault.InvalidValue("agent_execution", "ended execution has invalid terminal fields")
	}
	if !agentmodel.ValidFailureCode(e.FailureCode) || e.FailureCode == agentmodel.ExecutionFailureRequestCanceled && e.Outcome != agentmodel.ExecutionPaused && e.Outcome != agentmodel.ExecutionInterrupted {
		return fault.InvalidValue("execution.failureCode", "failure code does not match outcome")
	}
	if len([]rune(e.FailureMessage)) > agentmodel.MaxExecutionFailureMessageRunes || e.FailureMessage != "" && (e.Status != agentmodel.ExecutionEnded || e.Outcome != agentmodel.ExecutionFailed) {
		return fault.InvalidValue("execution.failureMessage", "failure message is invalid")
	}
	return nil
}

func validateExecutionConfig(s agentmodel.ExecutionConfig) error {
	if !definitionIDPattern.MatchString(s.AgentDefinitionID.String()) {
		return fault.InvalidValue("execution.config.agentDefinition", "definition id is required")
	}
	if err := validateExecutionModel(s.Model); err != nil {
		return fault.FieldError("execution.config.model", err)
	}
	if !pathutil.IsAbsoluteNormalized(s.WorkspacePath) {
		return fault.InvalidValue("execution.config.workspacePath", "workspace path must be an absolute normalized path")
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

func validateContextEntry(e sessionmodel.SessionContextEntry) error {
	if e.SourceExecutionID.String() != strings.TrimSpace(e.SourceExecutionID.String()) {
		return fault.InvalidValue("sessionContext.sourceExecutionID", "source task id must be canonical")
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
