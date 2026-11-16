package role

// Role 表示上下文条目的参与方。
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	Tool      Role = "tool"
)
