package session

import (
	"context"
	"errors"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

func validateCatalog(ctx context.Context, catalog sessionmodel.BranchCatalog, log *eventLog) error {
	branches := make(map[identity.BranchID]sessionmodel.Branch, len(catalog.Branches))
	children := make(map[identity.BranchID][]identity.BranchID)
	var bases []string
	for _, branch := range catalog.Branches {
		branches[branch.ID] = branch
		if branch.IsRoot() {
			if branch.ID != catalog.RootBranchID {
				return errors.New("branch catalog contains an unregistered root")
			}
		} else {
			children[branch.SourceBranchID] = append(children[branch.SourceBranchID], branch.ID)
			bases = append(bases, branch.BaseEntryID)
		}
	}
	root, found := branches[catalog.RootBranchID]
	if !found || !root.IsRoot() {
		return errors.New("branch catalog has no valid root")
	}
	if _, found := branches[catalog.DefaultBranchID]; !found {
		return errors.New("branch catalog has no valid default selection")
	}
	pending := []identity.BranchID{root.ID}
	count := 0
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		count++
		pending = append(pending, children[id]...)
	}
	if count != len(branches) {
		return errors.New("branch catalog is not a single rooted tree")
	}
	locations, err := log.store.EntryLocations(ctx, bases)
	if err != nil {
		return err
	}
	owners := make(map[string]identity.BranchID)
	for _, location := range locations {
		if location.Node.Run && location.Node.AgentID == identity.AgentID(log.sessionID) {
			owners[location.Node.ID] = location.Node.BranchID
		}
	}
	for _, branch := range catalog.Branches {
		if !branch.IsRoot() && owners[branch.BaseEntryID] != branch.SourceBranchID {
			return fmt.Errorf("branch %s has an invalid fork owner or end node", branch.ID)
		}
	}
	return nil
}
