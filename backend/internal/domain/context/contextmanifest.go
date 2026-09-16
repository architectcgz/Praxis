package context

import (
	"strings"
	"time"
)

type ContextManifest struct {
	ID                ContextManifestID
	SessionRevision   uint64
	EntryIDs          []ContextEntryID
	ArtifactEntryRefs []string
	SelectionDigest   string
	Sources           []ContentRef
	CreatedAt         time.Time
}

func NewContextManifest(
	id ContextManifestID,
	sessionRevision uint64,
	entryIDs []ContextEntryID,
	artifactEntryRefs []string,
	selectionDigest string,
	sources []ContentRef,
	createdAt time.Time,
) (ContextManifest, error) {
	manifest := ContextManifest{
		ID: id, SessionRevision: sessionRevision,
		EntryIDs: cloneContextEntryIDs(entryIDs), ArtifactEntryRefs: cloneStrings(artifactEntryRefs),
		SelectionDigest: selectionDigest,
		Sources:         cloneContentRefs(sources), CreatedAt: createdAt.UTC(),
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
	if m.SessionRevision == 0 || len(m.EntryIDs) == 0 || idIsEmpty(m.SelectionDigest) {
		return invalidValue("contextManifest", "revision, selected entries and digest are required")
	}
	seen := make(map[ContextEntryID]struct{}, len(m.EntryIDs))
	for _, id := range m.EntryIDs {
		if idIsEmpty(string(id)) {
			return invalidValue("contextManifest.entryIDs", "entry id is required")
		}
		if _, ok := seen[id]; ok {
			return invalidValue("contextManifest.entryIDs", "entry id is duplicated")
		}
		seen[id] = struct{}{}
	}
	seenArtifacts := make(map[string]struct{}, len(m.ArtifactEntryRefs))
	for _, ref := range m.ArtifactEntryRefs {
		if idIsEmpty(ref) || strings.ContainsAny(ref, "\x00\r\n") {
			return invalidValue("contextManifest.artifactEntryRefs", "artifact entry reference is invalid")
		}
		if _, ok := seenArtifacts[ref]; ok {
			return invalidValue("contextManifest.artifactEntryRefs", "artifact entry reference is duplicated")
		}
		seenArtifacts[ref] = struct{}{}
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

func (m ContextManifest) Snapshot() ContextManifest {
	m.EntryIDs = cloneContextEntryIDs(m.EntryIDs)
	m.ArtifactEntryRefs = cloneStrings(m.ArtifactEntryRefs)
	m.Sources = cloneContentRefs(m.Sources)
	return m
}
