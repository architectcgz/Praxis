package jsonl

import (
	"errors"
	"testing"
)

type failingLogFile struct {
	data                                       []byte
	writeFailure, syncFailure, truncateFailure bool
	syncs                                      int
}

func (f *failingLogFile) WriteAt(data []byte, offset int64) (int, error) {
	if f.writeFailure {
		f.data = append(f.data[:offset], data[:len(data)/2]...)
		return len(data) / 2, errors.New("write failed")
	}
	f.data = append(f.data[:offset], data...)
	return len(data), nil
}
func (f *failingLogFile) Sync() error {
	f.syncs++
	if f.syncFailure && f.syncs == 1 {
		return errors.New("sync failed")
	}
	return nil
}
func (f *failingLogFile) Truncate(size int64) error {
	if f.truncateFailure {
		return errors.New("truncate failed")
	}
	f.data = f.data[:size]
	return nil
}
func TestAppendRollbackAndFault(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		write, sync, rollback bool
	}{
		{name: "partial_write", write: true}, {name: "sync", sync: true}, {name: "rollback_failure", write: true, rollback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Store{}
			file := &failingLogFile{
				data:            []byte("committed\n"),
				writeFailure:    tc.write,
				syncFailure:     tc.sync,
				truncateFailure: tc.rollback,
			}
			err := s.appendTo(file, int64(len(file.data)), []byte("new record\n"))
			if err == nil || (s.fault != nil) != tc.rollback {
				t.Fatal(err, s.fault)
			}
			if !tc.rollback && string(file.data) != "committed\n" {
				t.Fatal("failed write must truncate and sync to the original commit boundary")
			}
		})
	}
}
