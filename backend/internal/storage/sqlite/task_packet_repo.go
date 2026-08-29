package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetTaskPacket(ctx context.Context, id domain.TaskPacketID) (domain.TaskPacket, error) {
	var value domain.TaskPacket
	return value, s.loadPayload(ctx, "task_packets", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveTaskPacket(ctx context.Context, value domain.TaskPacket) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(ctx, `INSERT INTO task_packets (id, payload) VALUES (?, ?)
        ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`, value.ID.String(), payload)
}

type TaskPacketRepository struct{ store *Store }

func (r TaskPacketRepository) Get(ctx context.Context, id domain.TaskPacketID) (domain.TaskPacket, error) {
	return r.store.GetTaskPacket(ctx, id)
}

func (r TaskPacketRepository) Save(ctx context.Context, value domain.TaskPacket) error {
	return r.store.SaveTaskPacket(ctx, value)
}
