package jsonl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

func (s *Store) replay(ctx context.Context, scope string) error {
	path, err := s.path(ctx, scope)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("log must be a regular file: %s", path)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	messages := newSessionMessages()
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 {
				return s.truncateTail(file, offset, line)
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
		var record batch
		if err := decodeJSON(line, &record); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		if len(record.Events) == 0 {
			return fmt.Errorf("%s offset %d: empty commit", path, offset)
		}
		tx := s.newTransaction()
		tx.scope, tx.nextSequence, tx.replayMessages = scope, s.sequences[scope], messages
		txCtx := context.WithValue(ctx, transactionKey{}, tx)
		for _, event := range record.Events {
			var entry indexEntry
			if event.Type != "session.deleted" {
				entry, err = restoreIndexEntry(scope, event)
				if err != nil {
					return fmt.Errorf("%s seq=%d: %w", path, event.Sequence, err)
				}
			}
			if err := s.stage(txCtx, event, entry); err != nil {
				return fmt.Errorf("%s seq=%d: %w", path, event.Sequence, err)
			}
		}
		if len(tx.events) != len(record.Events) {
			return fmt.Errorf("%s offset %d: redundant or duplicate event", path, offset)
		}
		if err := s.validateTransaction(txCtx); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		s.publish(tx, offset, int64(len(line)))
		tx.active = false
		if tx.deletedScope {
			messages = newSessionMessages()
		}
		offset += int64(len(line))
	}
}

func (s *Store) truncateTail(file *os.File, offset int64, tail []byte) error {
	backup, err := os.CreateTemp(s.root, "incomplete-tail-*")
	if err != nil {
		return err
	}
	_, writeErr := backup.Write(tail)
	err = errors.Join(writeErr, backup.Sync(), backup.Close())
	if err != nil {
		return err
	}
	if err := syncDirectory(s.root); err != nil {
		return err
	}
	if err := file.Truncate(offset); err != nil {
		return err
	}
	return file.Sync()
}
