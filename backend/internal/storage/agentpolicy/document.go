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
	domainsecurity "praxis/internal/core/domain/security"
)

const documentVersion = 1

type document struct {
	Version      int                                          `json:"version"`
	ApprovalMode domainsecurity.ApprovalMode                  `json:"approvalMode"`
	SandboxMode  domainsecurity.SandboxMode                   `json:"sandboxMode"`
	Profiles     map[domainsecurity.AgentProfile]jsonTemplate `json:"profiles"`
}

type jsonTemplate struct {
	AllowedTools      []domainsecurity.ToolName         `json:"allowedTools"`
	WorkspaceAccess   domainsecurity.WorkspaceAccess    `json:"workspaceAccess"`
	ResultPermissions []domainsecurity.ResultPermission `json:"resultPermissions"`
}

// load falls back to the built-in conservative policy when the user has never
// written a policy file; any other read or parse failure is fatal so a corrupt
// document cannot silently widen permissions.
func load(path string) (domainsecurity.AgentPolicySnapshot, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultSnapshot()
	}
	if err != nil {
		return domainsecurity.AgentPolicySnapshot{}, fmt.Errorf("read agent policy: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var parsed document
	if err := decoder.Decode(&parsed); err != nil {
		return domainsecurity.AgentPolicySnapshot{}, fmt.Errorf("contract error: parse agent policy: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return domainsecurity.AgentPolicySnapshot{}, fmt.Errorf("contract error: parse agent policy: %w", err)
	}
	return snapshotFromDocument(parsed, revision(encoded))
}

func snapshotFromDocument(parsed document, rev string) (domainsecurity.AgentPolicySnapshot, error) {
	if parsed.Version != documentVersion {
		return domainsecurity.AgentPolicySnapshot{}, fmt.Errorf(
			"contract error: agent policy version %d is unsupported",
			parsed.Version,
		)
	}
	profiles := defaultTemplates()
	for profile, template := range parsed.Profiles {
		if !profile.Valid() {
			return domainsecurity.AgentPolicySnapshot{}, fmt.Errorf("contract error: unknown agent profile %q", profile)
		}
		resolved, err := domainsecurity.NewDefaultGrantTemplate(
			template.AllowedTools,
			template.WorkspaceAccess,
			template.ResultPermissions,
		)
		if err != nil {
			return domainsecurity.AgentPolicySnapshot{}, fmt.Errorf("contract error: invalid %s template: %w", profile, err)
		}
		profiles[profile] = resolved
	}
	snapshot, err := domainsecurity.NewAgentPolicySnapshot(rev, parsed.ApprovalMode, parsed.SandboxMode, profiles)
	if err != nil {
		return domainsecurity.AgentPolicySnapshot{}, fmt.Errorf("contract error: invalid agent policy: %w", err)
	}
	return snapshot, nil
}

func documentFromSnapshot(snapshot domainsecurity.AgentPolicySnapshot) document {
	profiles := make(map[domainsecurity.AgentProfile]jsonTemplate, len(snapshot.Profiles))
	for profile, template := range snapshot.Profiles {
		profiles[profile] = jsonTemplate{
			AllowedTools:      append([]domainsecurity.ToolName(nil), template.AllowedTools...),
			WorkspaceAccess:   template.WorkspaceAccess,
			ResultPermissions: append([]domainsecurity.ResultPermission(nil), template.ResultPermissions...),
		}
	}
	return document{
		Version:      documentVersion,
		ApprovalMode: snapshot.ApprovalMode,
		SandboxMode:  snapshot.SandboxMode,
		Profiles:     profiles,
	}
}

func defaultSnapshot() (domainsecurity.AgentPolicySnapshot, error) {
	return domainsecurity.NewAgentPolicySnapshot(
		"builtin-v1",
		domainsecurity.ApprovalAlwaysAsk,
		domainsecurity.SandboxReadOnly,
		defaultTemplates(),
	)
}

// defaultTemplates is the conservative baseline applied to every profile the
// user document omits: read-only workspace access and no mutating tools.
func defaultTemplates() map[domainsecurity.AgentProfile]domainsecurity.DefaultGrantTemplate {
	readTools := []domainsecurity.ToolName{domainsecurity.ToolReadFile, domainsecurity.ToolListDir, domainsecurity.ToolSearchText}
	resultTools := []domainsecurity.ToolName{domainsecurity.ToolSubmitResult, domainsecurity.ToolSubmitBriefing}
	resultPermissions := []domainsecurity.ResultPermission{domainsecurity.ResultPermissionAgentResult, domainsecurity.ResultPermissionBriefing}
	primary, _ := domainsecurity.NewDefaultGrantTemplate(
		append(
			append([]domainsecurity.ToolName{}, readTools...),
			append([]domainsecurity.ToolName{domainsecurity.ToolProposeDelegate}, resultTools...)...),
		domainsecurity.WorkspaceAccessRead,
		resultPermissions,
	)
	delegate, _ := domainsecurity.NewDefaultGrantTemplate(
		append(append([]domainsecurity.ToolName{}, readTools...), resultTools...),
		domainsecurity.WorkspaceAccessRead,
		resultPermissions,
	)
	advisor, _ := domainsecurity.NewDefaultGrantTemplate(
		append(append([]domainsecurity.ToolName{}, readTools...), resultTools...),
		domainsecurity.WorkspaceAccessRead,
		resultPermissions,
	)
	curator, _ := domainsecurity.NewDefaultGrantTemplate(
		[]domainsecurity.ToolName{domainsecurity.ToolSubmitResult},
		domainsecurity.WorkspaceAccessNone,
		[]domainsecurity.ResultPermission{domainsecurity.ResultPermissionAgentResult},
	)
	return map[domainsecurity.AgentProfile]domainsecurity.DefaultGrantTemplate{
		domainsecurity.ProfilePrimary:  primary,
		domainsecurity.ProfileDelegate: delegate,
		domainsecurity.ProfileAdvisor:  advisor,
		domainsecurity.ProfileCurator:  curator,
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
