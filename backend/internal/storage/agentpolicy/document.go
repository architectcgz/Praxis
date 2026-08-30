package agentpolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"praxis/internal/core/domain"
)

const documentVersion = 1

type document struct {
	Version      int                                  `json:"version"`
	ApprovalMode domain.ApprovalMode                  `json:"approvalMode"`
	SandboxMode  domain.SandboxMode                   `json:"sandboxMode"`
	Profiles     map[domain.AgentProfile]jsonTemplate `json:"profiles"`
}

type jsonTemplate struct {
	AllowedTools      []domain.ToolName         `json:"allowedTools"`
	WorkspaceAccess   domain.WorkspaceAccess    `json:"workspaceAccess"`
	ResultPermissions []domain.ResultPermission `json:"resultPermissions"`
}

// load falls back to the built-in conservative policy when the user has never
// written a policy file; any other read or parse failure is fatal so a corrupt
// document cannot silently widen permissions.
func load(path string) (domain.AgentPolicySnapshot, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultSnapshot()
	}
	if err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("read agent policy: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var parsed document
	if err := decoder.Decode(&parsed); err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: parse agent policy: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: parse agent policy: %w", err)
	}
	return snapshotFromDocument(parsed, revision(encoded))
}

func snapshotFromDocument(parsed document, rev string) (domain.AgentPolicySnapshot, error) {
	if parsed.Version != documentVersion {
		return domain.AgentPolicySnapshot{}, fmt.Errorf(
			"contract error: agent policy version %d is unsupported",
			parsed.Version,
		)
	}
	profiles := defaultTemplates()
	for profile, template := range parsed.Profiles {
		if !profile.Valid() {
			return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: unknown agent profile %q", profile)
		}
		resolved, err := domain.NewDefaultGrantTemplate(
			template.AllowedTools,
			template.WorkspaceAccess,
			template.ResultPermissions,
		)
		if err != nil {
			return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: invalid %s template: %w", profile, err)
		}
		profiles[profile] = resolved
	}
	snapshot, err := domain.NewAgentPolicySnapshot(rev, parsed.ApprovalMode, parsed.SandboxMode, profiles)
	if err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: invalid agent policy: %w", err)
	}
	return snapshot, nil
}

func documentFromSnapshot(snapshot domain.AgentPolicySnapshot) document {
	profiles := make(map[domain.AgentProfile]jsonTemplate, len(snapshot.Profiles))
	for profile, template := range snapshot.Profiles {
		profiles[profile] = jsonTemplate{
			AllowedTools:      append([]domain.ToolName(nil), template.AllowedTools...),
			WorkspaceAccess:   template.WorkspaceAccess,
			ResultPermissions: append([]domain.ResultPermission(nil), template.ResultPermissions...),
		}
	}
	return document{
		Version:      documentVersion,
		ApprovalMode: snapshot.ApprovalMode,
		SandboxMode:  snapshot.SandboxMode,
		Profiles:     profiles,
	}
}

func defaultSnapshot() (domain.AgentPolicySnapshot, error) {
	return domain.NewAgentPolicySnapshot(
		"builtin-v1",
		domain.ApprovalAlwaysAsk,
		domain.SandboxReadOnly,
		defaultTemplates(),
	)
}

// defaultTemplates is the conservative baseline applied to every profile the
// user document omits: read-only workspace access and no mutating tools.
func defaultTemplates() map[domain.AgentProfile]domain.DefaultGrantTemplate {
	readTools := []domain.ToolName{domain.ToolReadFile, domain.ToolListDir, domain.ToolSearchText}
	resultTools := []domain.ToolName{domain.ToolSubmitResult, domain.ToolSubmitBriefing}
	resultPermissions := []domain.ResultPermission{domain.ResultPermissionAgentResult, domain.ResultPermissionBriefing}
	primary, _ := domain.NewDefaultGrantTemplate(
		append(
			append([]domain.ToolName{}, readTools...),
			append([]domain.ToolName{domain.ToolProposeDelegate}, resultTools...)...),
		domain.WorkspaceAccessRead,
		resultPermissions,
	)
	delegate, _ := domain.NewDefaultGrantTemplate(
		append(append([]domain.ToolName{}, readTools...), resultTools...),
		domain.WorkspaceAccessRead,
		resultPermissions,
	)
	advisor, _ := domain.NewDefaultGrantTemplate(
		append(append([]domain.ToolName{}, readTools...), resultTools...),
		domain.WorkspaceAccessRead,
		resultPermissions,
	)
	curator, _ := domain.NewDefaultGrantTemplate(
		[]domain.ToolName{domain.ToolSubmitResult},
		domain.WorkspaceAccessNone,
		[]domain.ResultPermission{domain.ResultPermissionAgentResult},
	)
	return map[domain.AgentProfile]domain.DefaultGrantTemplate{
		domain.ProfilePrimary:  primary,
		domain.ProfileDelegate: delegate,
		domain.ProfileAdvisor:  advisor,
		domain.ProfileCurator:  curator,
	}
}

// revision identifies the exact bytes a snapshot came from, so an execution can
// record which policy document authorized it.
func revision(contents []byte) string {
	sum := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values are not allowed")
	}
	return err
}
