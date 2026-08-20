package domain

import (
	"strings"
	"time"
)

const MaxManifestSummaryBytes = 16 * 1024

type ContextManifest struct {
	ID        ContextManifestID
	Summary   string
	Sources   []ContentRef
	CreatedAt time.Time
}

func NewContextManifest(
	id ContextManifestID,
	summary string,
	sources []ContentRef,
	createdAt time.Time,
) (ContextManifest, error) {
	manifest := ContextManifest{
		ID:        id,
		Summary:   strings.TrimSpace(summary),
		Sources:   cloneContentRefs(sources),
		CreatedAt: createdAt.UTC(),
	}
	if err := manifest.Validate(); err != nil {
		return ContextManifest{}, err
	}
	return manifest, nil
}

func (m ContextManifest) Validate() error {
	if idIsEmpty(string(m.ID)) {
		return invalidValue("contextManifest.id", "id is required")
	}
	if m.Summary == "" {
		return invalidValue("contextManifest.summary", "summary is required")
	}
	if len([]byte(m.Summary)) > MaxManifestSummaryBytes {
		return invalidValue("contextManifest.summary", "summary exceeds the bounded context limit")
	}
	if m.CreatedAt.IsZero() {
		return invalidValue("contextManifest.createdAt", "createdAt is required")
	}
	if len(m.Sources) > MaxContentRefs {
		return invalidValue("contextManifest.sources", "too many content references")
	}
	for _, source := range m.Sources {
		if err := source.Validate(); err != nil {
			return fmtField("contextManifest.sources", err)
		}
	}
	return nil
}

func (m ContextManifest) References() []ContentRef { return cloneContentRefs(m.Sources) }
