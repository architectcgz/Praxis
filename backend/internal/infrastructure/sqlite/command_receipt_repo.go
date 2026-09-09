package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domaincommand "praxis/internal/domain/command"
	domainfoundation "praxis/internal/domain/foundation"
)

func (s *Store) GetCommandReceipt(ctx context.Context, requestID domainfoundation.RequestID) (domaincommand.CommandReceipt, error) {
	row := executorFromContext(ctx, s.db).QueryRowContext(ctx,
		`SELECT request_id, command, arguments_digest, result_payload, created_at
		 FROM command_receipts WHERE request_id = ?`, requestID.String())
	var receipt domaincommand.CommandReceipt
	var payload []byte
	var created string
	if err := row.Scan(&requestID, &receipt.Command, &receipt.ArgumentsDigest, &payload, &created); errors.Is(err, sql.ErrNoRows) {
		return domaincommand.CommandReceipt{}, domainfoundation.ErrNotFound
	} else if err != nil {
		return domaincommand.CommandReceipt{}, fmt.Errorf("read command receipt: %w", err)
	}
	receipt.RequestID = requestID
	if err := json.Unmarshal(payload, &receipt.ResultPayload); err != nil {
		return domaincommand.CommandReceipt{}, fmt.Errorf("decode command receipt: %w", err)
	}
	value, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domaincommand.CommandReceipt{}, fmt.Errorf("parse command receipt timestamp: %w", err)
	}
	receipt.CreatedAt = value
	if err := receipt.Validate(); err != nil {
		return domaincommand.CommandReceipt{}, err
	}
	return receipt, nil
}

func (s *Store) SaveCommandReceipt(ctx context.Context, receipt domaincommand.CommandReceipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(receipt.ResultPayload)
	if err != nil {
		return fmt.Errorf("encode command receipt: %w", err)
	}
	return s.savePayload(ctx,
		`INSERT INTO command_receipts (request_id, command, arguments_digest, result_payload, created_at)
		 VALUES (?, ?, ?, ?, ?)`, receipt.RequestID.String(), receipt.Command,
		receipt.ArgumentsDigest, payload, receipt.CreatedAt.UTC().Format(time.RFC3339Nano))
}

type CommandReceiptRepository struct{ store *Store }

func (r CommandReceiptRepository) Get(ctx context.Context, id domainfoundation.RequestID) (domaincommand.CommandReceipt, error) {
	return r.store.GetCommandReceipt(ctx, id)
}

func (r CommandReceiptRepository) Save(ctx context.Context, value domaincommand.CommandReceipt) error {
	return r.store.SaveCommandReceipt(ctx, value)
}
