package contracts

// IDs are distinct types so a repository cannot accidentally swap identifiers
// between aggregates while all persistence formats can still use strings.
type (
	ProjectID             string
	WorkspaceID           string
	SessionID             string
	AgentDefinitionID     string
	AgentID               string
	TurnID                string
	ToolInvocationID      string
	RequestID             string
	WaitConditionID       string
	AgentControlCommandID string
	WorkItemID            string
	ContextEntryID        string
)

func (id ProjectID) String() string         { return string(id) }
func (id WorkspaceID) String() string       { return string(id) }
func (id SessionID) String() string         { return string(id) }
func (id AgentDefinitionID) String() string { return string(id) }
func (id AgentID) String() string           { return string(id) }
func (id TurnID) String() string            { return string(id) }
func (id ToolInvocationID) String() string  { return string(id) }
func (id RequestID) String() string         { return string(id) }
func (id WaitConditionID) String() string   { return string(id) }
func (id AgentControlCommandID) String() string {
	return string(id)
}
func (id WorkItemID) String() string     { return string(id) }
func (id ContextEntryID) String() string { return string(id) }
