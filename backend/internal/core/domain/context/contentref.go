package context

import (
	"strings"
	"unicode/utf8"
)

const (
	MaxContentExcerptBytes = 32 * 1024
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
