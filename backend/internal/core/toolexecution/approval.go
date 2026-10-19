package toolexecution

import (
	"time"
)

type ApprovalSource string

const (
	ApprovalSourceUser ApprovalSource = "user"
	ApprovalSourceYolo ApprovalSource = "yolo"
	ApprovalSourceAuto ApprovalSource = "auto"
)

type ApprovalRecord struct {
	Source          ApprovalSource
	ArgumentsDigest string
	ApprovedAt      time.Time
}
