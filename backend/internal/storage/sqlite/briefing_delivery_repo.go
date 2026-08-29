package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
)

func (s *Store) GetDelivery(ctx context.Context, id domain.DeliveryID) (domain.BriefingDelivery, error) {
	var value domain.BriefingDelivery
	return value, s.loadPayload(
		ctx,
		"briefing_deliveries",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) GetDeliveryByInjectionKey(ctx context.Context, injectionKey string) (domain.BriefingDelivery, error) {
	row := executorFromContext(
		ctx,
		s.db,
	).QueryRowContext(ctx, `SELECT payload FROM briefing_deliveries WHERE injection_key = ?`, injectionKey)
	value, err := decodePayload[domain.BriefingDelivery](
		row,
		func(value domain.BriefingDelivery) error { return value.Validate() },
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.BriefingDelivery{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.BriefingDelivery{}, fmt.Errorf("get delivery by injection key: %w", err)
	}
	return value, nil
}

func (s *Store) SaveDelivery(ctx context.Context, value domain.BriefingDelivery) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO briefing_deliveries (
	        id, briefing_id, target_thread_id, injection_key, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        briefing_id = excluded.briefing_id,
	        target_thread_id = excluded.target_thread_id,
	        injection_key = excluded.injection_key,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.BriefingID.String(),
		value.TargetThreadID.String(),
		value.InjectionKey,
		string(value.Status),
		payload,
	)
	if err != nil && isConstraintError(err, "briefing_deliveries.injection_key") {
		return fmt.Errorf("%w: %s", domain.ErrAlreadyDelivered, value.InjectionKey)
	}
	return err
}

type DeliveryRepository struct{ store *Store }

func (r DeliveryRepository) Get(ctx context.Context, id domain.DeliveryID) (domain.BriefingDelivery, error) {
	return r.store.GetDelivery(ctx, id)
}

func (r DeliveryRepository) GetByInjectionKey(ctx context.Context, key string) (domain.BriefingDelivery, error) {
	return r.store.GetDeliveryByInjectionKey(ctx, key)
}

func (r DeliveryRepository) Save(ctx context.Context, value domain.BriefingDelivery) error {
	return r.store.SaveDelivery(ctx, value)
}
