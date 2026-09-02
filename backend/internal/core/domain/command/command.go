package command

import "time"

// CommandReceipt records the durable identity and argument fingerprint of a
// state-changing command. ResultPayload contains only safe identifiers.
type CommandReceipt struct {
	RequestID       RequestID
	Command         string
	ArgumentsDigest string
	ResultPayload   []byte
	CreatedAt       time.Time
}

func (r CommandReceipt) Validate() error {
	if idIsEmpty(string(r.RequestID)) || r.Command == "" || r.ArgumentsDigest == "" {
		return invalidValue("commandReceipt", "request identity and arguments are required")
	}
	if r.CreatedAt.IsZero() {
		return invalidValue("commandReceipt.createdAt", "createdAt is required")
	}
	return nil
}

func (r CommandReceipt) Snapshot() CommandReceipt {
	copy := r
	copy.ResultPayload = append([]byte(nil), r.ResultPayload...)
	return copy
}
