package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domaincommand "praxis/internal/domain/command"
	domainfoundation "praxis/internal/domain/foundation"
)

func (s *Store) GetCommandReceipt(ctx context.Context, requestID domainfoundation.RequestID) (domaincommand.CommandReceipt, error) {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT request_id, command, arguments_digest, result_ref, created_at FROM command_receipts WHERE request_id = ?`, requestID.String())
	var receipt domaincommand.CommandReceipt
	var ref, createdAt string
	if err := row.Scan(&receipt.RequestID, &receipt.Command, &receipt.ArgumentsDigest, &ref, &createdAt); err == sql.ErrNoRows {
		return receipt, domainfoundation.ErrNotFound
	} else if err != nil {
		return receipt, fmt.Errorf("read command receipt: %w", err)
	}
	if err := s.loadDocument(ctx, ref, &receipt.ResultPayload, "command receipt", nil); err != nil {
		return receipt, err
	}
	var err error
	receipt.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return receipt, err
	}
	return receipt, receipt.Validate()
}
func (s *Store) SaveCommandReceipt(ctx context.Context, receipt domaincommand.CommandReceipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "command", receipt.RequestID.String(), receipt.ResultPayload)
	if err != nil {
		return err
	}
	return s.saveMetadata(ctx, `INSERT INTO command_receipts (request_id, command, arguments_digest, result_ref, created_at) VALUES (?, ?, ?, ?, ?)`, receipt.RequestID.String(), receipt.Command, receipt.ArgumentsDigest, ref, receipt.CreatedAt.UTC().Format(time.RFC3339Nano))
}

type CommandReceiptRepository struct{ store *Store }

func (r CommandReceiptRepository) Get(ctx context.Context, id domainfoundation.RequestID) (domaincommand.CommandReceipt, error) {
	return r.store.GetCommandReceipt(ctx, id)
}
func (r CommandReceiptRepository) Save(ctx context.Context, value domaincommand.CommandReceipt) error {
	return r.store.SaveCommandReceipt(ctx, value)
}
