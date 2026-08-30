package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetCapabilityGrant(ctx context.Context, id domain.CapabilityGrantID) (domain.CapabilityGrant, error) {
	var value domain.CapabilityGrant
	return value, s.loadPayload(ctx, "capability_grants", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveCapabilityGrant(ctx context.Context, value domain.CapabilityGrant) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO capability_grants (
	        id, workspace_id, workspace_path_snapshot, workspace_revision,
	        context_manifest_id, approval_source, approval_policy_fingerprint, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        workspace_id = excluded.workspace_id,
	        workspace_path_snapshot = excluded.workspace_path_snapshot,
	        workspace_revision = excluded.workspace_revision,
	        context_manifest_id = excluded.context_manifest_id,
	        approval_source = excluded.approval_source,
	        approval_policy_fingerprint = excluded.approval_policy_fingerprint,
	        payload = excluded.payload`,
		value.ID.String(),
		value.WorkspaceID.String(),
		value.WorkspacePathSnapshot,
		value.WorkspaceRevision,
		value.ContextManifestRef.String(),
		string(value.ApprovalSource),
		value.ApprovalPolicyFingerprint,
		payload,
	)
}

type CapabilityGrantRepository struct{ store *Store }

func (r CapabilityGrantRepository) Get(
	ctx context.Context,
	id domain.CapabilityGrantID,
) (domain.CapabilityGrant, error) {
	return r.store.GetCapabilityGrant(ctx, id)
}

func (r CapabilityGrantRepository) Save(ctx context.Context, value domain.CapabilityGrant) error {
	return r.store.SaveCapabilityGrant(ctx, value)
}
