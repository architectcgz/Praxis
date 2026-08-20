package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"praxis/internal/core/domain"
)

const agentPolicyVersion = 1

// AgentPolicyStore owns local policy-file validation and replacement. It never
// materializes a CapabilityGrant, which remains an orchestration concern.
type AgentPolicyStore struct {
	path  string
	mu    sync.RWMutex
	cache *domain.AgentPolicySnapshot
}

type agentPolicyDocument struct {
	Version      int                                         `json:"version"`
	ApprovalMode domain.ApprovalMode                         `json:"approvalMode"`
	SandboxMode  domain.SandboxMode                          `json:"sandboxMode"`
	Profiles     map[domain.AgentProfile]agentPolicyTemplate `json:"profiles"`
}

type agentPolicyTemplate struct {
	AllowedTools      []domain.ToolName         `json:"allowedTools"`
	WorkspaceAccess   domain.WorkspaceAccess    `json:"workspaceAccess"`
	ResultPermissions []domain.ResultPermission `json:"resultPermissions"`
}

// NewAgentPolicyStore returns a store for one DataRoot. Current loads lazily
// so an application can construct its composition graph before filesystem I/O.
func NewAgentPolicyStore(path string) (*AgentPolicyStore, error) {
	if filepath.Base(path) != "agent-policy.json" || !filepath.IsAbs(path) {
		return nil, errors.New("agent policy path must be an absolute agent-policy.json path")
	}
	return &AgentPolicyStore{path: path}, nil
}

// Current returns the effective policy, including conservative defaults for
// profiles omitted from the user-owned JSON document.
func (s *AgentPolicyStore) Current(ctx context.Context) (domain.AgentPolicySnapshot, error) {
	if ctx == nil {
		return domain.AgentPolicySnapshot{}, errors.New("agent policy context is required")
	}
	if err := ctx.Err(); err != nil {
		return domain.AgentPolicySnapshot{}, err
	}
	s.mu.RLock()
	if s.cache != nil {
		value := s.cache.Snapshot()
		s.mu.RUnlock()
		return value, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		loaded, err := loadAgentPolicy(s.path)
		if err != nil {
			return domain.AgentPolicySnapshot{}, err
		}
		s.cache = &loaded
	}
	return s.cache.Snapshot(), nil
}

// Update atomically persists a fully validated policy. The cache changes only
// after rename succeeds, so a failed write cannot widen later approvals.
func (s *AgentPolicyStore) Update(ctx context.Context, snapshot domain.AgentPolicySnapshot) error {
	if ctx == nil {
		return errors.New("agent policy context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("contract error: invalid agent policy: %w", err)
	}
	document := policyDocumentFromSnapshot(snapshot)
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode agent policy: %w", err)
	}
	if err := writePrivateAtomically(s.path, append(encoded, '\n')); err != nil {
		return err
	}
	effective, err := snapshotFromDocument(document, policyRevision(encoded))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = &effective
	s.mu.Unlock()
	return nil
}

func loadAgentPolicy(path string) (domain.AgentPolicySnapshot, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultAgentPolicy()
	}
	if err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("read agent policy: %w", err)
	}
	decoder := json.NewDecoder(bytesReader(encoded))
	decoder.DisallowUnknownFields()
	var document agentPolicyDocument
	if err := decoder.Decode(&document); err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: parse agent policy: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: parse agent policy: %w", err)
	}
	return snapshotFromDocument(document, policyRevision(encoded))
}

func snapshotFromDocument(document agentPolicyDocument, revision string) (domain.AgentPolicySnapshot, error) {
	if document.Version != agentPolicyVersion {
		return domain.AgentPolicySnapshot{}, fmt.Errorf(
			"contract error: agent policy version %d is unsupported",
			document.Version,
		)
	}
	profiles := defaultPolicyTemplates()
	for profile, template := range document.Profiles {
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
	snapshot, err := domain.NewAgentPolicySnapshot(revision, document.ApprovalMode, document.SandboxMode, profiles)
	if err != nil {
		return domain.AgentPolicySnapshot{}, fmt.Errorf("contract error: invalid agent policy: %w", err)
	}
	return snapshot, nil
}

func policyDocumentFromSnapshot(snapshot domain.AgentPolicySnapshot) agentPolicyDocument {
	profiles := make(map[domain.AgentProfile]agentPolicyTemplate, len(snapshot.Profiles))
	for profile, template := range snapshot.Profiles {
		profiles[profile] = agentPolicyTemplate{
			AllowedTools:      append([]domain.ToolName(nil), template.AllowedTools...),
			WorkspaceAccess:   template.WorkspaceAccess,
			ResultPermissions: append([]domain.ResultPermission(nil), template.ResultPermissions...),
		}
	}
	return agentPolicyDocument{
		Version:      agentPolicyVersion,
		ApprovalMode: snapshot.ApprovalMode,
		SandboxMode:  snapshot.SandboxMode,
		Profiles:     profiles,
	}
}

func defaultAgentPolicy() (domain.AgentPolicySnapshot, error) {
	return domain.NewAgentPolicySnapshot(
		"builtin-v1",
		domain.ApprovalAlwaysAsk,
		domain.SandboxReadOnly,
		defaultPolicyTemplates(),
	)
}

func defaultPolicyTemplates() map[domain.AgentProfile]domain.DefaultGrantTemplate {
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
	consult, _ := domain.NewDefaultGrantTemplate(
		append(append([]domain.ToolName{}, readTools...), resultTools...),
		domain.WorkspaceAccessRead,
		resultPermissions,
	)
	note, _ := domain.NewDefaultGrantTemplate(
		[]domain.ToolName{domain.ToolSubmitResult},
		domain.WorkspaceAccessNone,
		[]domain.ResultPermission{domain.ResultPermissionAgentResult},
	)
	return map[domain.AgentProfile]domain.DefaultGrantTemplate{
		domain.ProfilePrimary:  primary,
		domain.ProfileDelegate: delegate,
		domain.ProfileConsult:  consult,
		domain.ProfileNote:     note,
	}
}

func policyRevision(contents []byte) string {
	sum := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writePrivateAtomically(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create policy directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agent-policy-*")
	if err != nil {
		return fmt.Errorf("create agent policy temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect agent policy temporary file: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write agent policy temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync agent policy temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close agent policy temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace agent policy: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect agent policy: %w", err)
	}
	return nil
}

func bytesReader(value []byte) *byteReader { return &byteReader{value: value} }

type byteReader struct{ value []byte }

func (r *byteReader) Read(target []byte) (int, error) {
	if len(r.value) == 0 {
		return 0, io.EOF
	}
	n := copy(target, r.value)
	r.value = r.value[n:]
	return n, nil
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

var _ interface {
	Current(context.Context) (domain.AgentPolicySnapshot, error)
	Update(context.Context, domain.AgentPolicySnapshot) error
} = (*AgentPolicyStore)(nil)
