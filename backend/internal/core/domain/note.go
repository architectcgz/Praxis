package domain

import (
	"strings"
	"time"
)

type Note struct {
	ID             NoteID
	TaskSessionID  TaskSessionID
	SourceThreadID AgentThreadID
	Title          string
	BodyRef        ContentRef
	Tags           []string
	CreatedAt      time.Time
}

func NewNote(
	id NoteID,
	sessionID TaskSessionID,
	sourceThreadID AgentThreadID,
	title string,
	bodyRef ContentRef,
	tags []string,
	at time.Time,
) (Note, error) {
	note := Note{
		ID:             id,
		TaskSessionID:  sessionID,
		SourceThreadID: sourceThreadID,
		Title:          strings.TrimSpace(title),
		BodyRef:        bodyRef,
		Tags:           cloneStrings(tags),
		CreatedAt:      at.UTC(),
	}
	for i := range note.Tags {
		note.Tags[i] = strings.TrimSpace(note.Tags[i])
	}
	if err := note.Validate(); err != nil {
		return Note{}, err
	}
	return note, nil
}

func (n Note) Validate() error {
	if idIsEmpty(string(n.ID)) || idIsEmpty(string(n.TaskSessionID)) || idIsEmpty(string(n.SourceThreadID)) {
		return invalidValue("note", "required reference is missing")
	}
	if n.Title == "" || len([]byte(n.Title)) > MaxArtifactSummaryBytes {
		return invalidValue("note.title", "title is empty or exceeds the bounded limit")
	}
	if err := n.BodyRef.Validate(); err != nil {
		return fmtField("note.bodyRef", err)
	}
	for _, tag := range n.Tags {
		if tag == "" || strings.ContainsAny(tag, "\x00\r\n") {
			return invalidValue("note.tags", "tag is empty or contains a control character")
		}
	}
	if n.CreatedAt.IsZero() {
		return invalidValue("note.createdAt", "creation time is required")
	}
	return nil
}

func (n Note) Snapshot() Note {
	copy := n
	copy.Tags = cloneStrings(n.Tags)
	return copy
}
