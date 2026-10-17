// Package security 定义 Bash 命令的审核策略，不提供进程或文件系统隔离。
package security

import (
	"slices"
	"strings"
)

type CommandMatch struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (m CommandMatch) Matches(command string) bool {
	switch m.Type {
	case "exact":
		return command == m.Value
	case "prefix":
		return strings.HasPrefix(command, m.Value)
	default:
		return false
	}
}

type CommandRule struct {
	ID     string       `json:"id"`
	Action ApprovalMode `json:"action"`
	Match  CommandMatch `json:"match"`
}

type CommandPolicy struct {
	Rules []CommandRule `json:"rules"`
}

func (p CommandPolicy) Clone() CommandPolicy {
	p.Rules = slices.Clone(p.Rules)
	return p
}

// Decide 优先匹配所有 deny，其余按配置顺序；未命中返回空决定，由统一入口解析默认动作。
func (p CommandPolicy) Decide(command string) ApprovalDecision {
	for _, rule := range p.Rules {
		if rule.Action == ApprovalDeny && rule.Match.Matches(command) {
			return ApprovalDecision{Action: rule.Action, RuleID: rule.ID}
		}
	}
	for _, rule := range p.Rules {
		if rule.Match.Matches(command) {
			return ApprovalDecision{Action: rule.Action, RuleID: rule.ID}
		}
	}
	return ApprovalDecision{}
}
