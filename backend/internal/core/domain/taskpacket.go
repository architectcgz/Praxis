package domain

import (
	"strings"
	"unicode/utf8"
)

const (
	MaxContentExcerptBytes = 32 * 1024
	MaxTaskGoalBytes       = 8 * 1024
	MaxAcceptanceItemBytes = 4 * 1024
	MaxAcceptanceItems     = 64
	MaxContentRefs         = 128
)

type ContentRefKind string

const (
	ContentRefFile     ContentRefKind = "file"
	ContentRefDiff     ContentRefKind = "diff"
	ContentRefLog      ContentRefKind = "log"
	ContentRefArtifact ContentRefKind = "artifact"
)

type ContentRef struct {
	Kind    ContentRefKind
	Path    string
	Digest  string
	Excerpt string
	Bytes   int64
}

func NewContentRef(kind ContentRefKind, path, digest, excerpt string, bytes int64) (ContentRef, error) {
	ref := ContentRef{
		Kind:    kind,
		Path:    strings.TrimSpace(path),
		Digest:  strings.TrimSpace(digest),
		Excerpt: excerpt,
		Bytes:   bytes,
	}
	if err := ref.Validate(); err != nil {
		return ContentRef{}, err
	}
	return ref, nil
}

func (r ContentRef) Validate() error {
	switch r.Kind {
	case ContentRefFile, ContentRefDiff, ContentRefLog, ContentRefArtifact:
	default:
		return invalidValue("contentRef.kind", "unsupported content reference kind")
	}
	if r.Path == "" && r.Digest == "" {
		return invalidValue("contentRef", "path or digest is required")
	}
	if strings.ContainsAny(r.Path, "\x00\r\n") {
		return invalidValue("contentRef.path", "path contains a control character")
	}
	if r.Digest != "" && strings.ContainsAny(r.Digest, "\x00\r\n") {
		return invalidValue("contentRef.digest", "digest contains a control character")
	}
	if !utf8.ValidString(r.Excerpt) {
		return invalidValue("contentRef.excerpt", "excerpt must be valid UTF-8")
	}
	if len([]byte(r.Excerpt)) > MaxContentExcerptBytes {
		return invalidValue("contentRef.excerpt", "excerpt exceeds the bounded context limit")
	}
	if r.Bytes < 0 {
		return invalidValue("contentRef.bytes", "byte count cannot be negative")
	}
	return nil
}

type TaskPacket struct {
	ID         TaskPacketID
	Goal       string
	Acceptance []string
	Refs       []ContentRef
}

func NewTaskPacket(id TaskPacketID, goal string, acceptance []string, refs []ContentRef) (TaskPacket, error) {
	packet := TaskPacket{
		ID:         id,
		Goal:       strings.TrimSpace(goal),
		Acceptance: cloneStrings(acceptance),
		Refs:       cloneContentRefs(refs),
	}
	for index, item := range packet.Acceptance {
		packet.Acceptance[index] = strings.TrimSpace(item)
	}
	if err := packet.Validate(); err != nil {
		return TaskPacket{}, err
	}
	return packet, nil
}

func (p TaskPacket) Validate() error {
	if idIsEmpty(string(p.ID)) {
		return invalidValue("taskPacket.id", "id is required")
	}
	if p.Goal == "" {
		return invalidValue("taskPacket.goal", "goal is required")
	}
	if len([]byte(p.Goal)) > MaxTaskGoalBytes {
		return invalidValue("taskPacket.goal", "goal exceeds the bounded context limit")
	}
	if len(p.Acceptance) > MaxAcceptanceItems {
		return invalidValue("taskPacket.acceptance", "too many acceptance items")
	}
	for _, item := range p.Acceptance {
		item = strings.TrimSpace(item)
		if item == "" {
			return invalidValue("taskPacket.acceptance", "acceptance item cannot be empty")
		}
		if len([]byte(item)) > MaxAcceptanceItemBytes {
			return invalidValue("taskPacket.acceptance", "acceptance item exceeds the bounded context limit")
		}
	}
	if len(p.Refs) > MaxContentRefs {
		return invalidValue("taskPacket.refs", "too many content references")
	}
	for _, ref := range p.Refs {
		if err := ref.Validate(); err != nil {
			return fmtField("taskPacket.refs", err)
		}
	}
	return nil
}

func (p TaskPacket) References() []ContentRef { return cloneContentRefs(p.Refs) }
