package entry

import (
	"errors"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/identity"
	"strings"
)

// Record 显式携带日志所属 Session 和消息所属 Agent，存储不依赖内存实例查询。
type Record struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	Data      ContextEntry
}

// Validate 校验日志中消息的自身身份和正文，不检查 Session 或 Execution 引用。
func (r Record) Validate() error {
	if r.AgentID == "" || r.AgentID.String() != strings.TrimSpace(r.AgentID.String()) || strings.ContainsAny(r.AgentID.String(), "\x00\r\n") {
		return errors.New("message agent identity must be canonical and nonempty")
	}
	if r.Data.Role == role.Assistant && (r.Data.AuthorKind != AuthorAgent || r.Data.AuthorID != r.AgentID.String()) {
		return errors.New("assistant author does not match the message agent")
	}
	return r.Data.Validate()
}
