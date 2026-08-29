package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetContextManifest(ctx context.Context, id domain.ContextManifestID) (domain.ContextManifest, error) {
	var value domain.ContextManifest
	return value, s.loadPayload(ctx, "context_manifests", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveContextManifest(ctx context.Context, value domain.ContextManifest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(ctx, `INSERT INTO context_manifests (id, payload) VALUES (?, ?)
        ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`, value.ID.String(), payload)
}

type ContextManifestRepository struct{ store *Store }

func (r ContextManifestRepository) Get(
	ctx context.Context,
	id domain.ContextManifestID,
) (domain.ContextManifest, error) {
	return r.store.GetContextManifest(ctx, id)
}

func (r ContextManifestRepository) Save(ctx context.Context, value domain.ContextManifest) error {
	return r.store.SaveContextManifest(ctx, value)
}
