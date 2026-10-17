// Package security 定义 Bash 命令的审核策略，不提供进程或文件系统隔离。
package security

import (
	"fmt"
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

// Validate 只读校验完整配置；匹配值保留空格和换行，以最终命令原文进行比较。
func (p CommandPolicy) Validate() error {
	seen := make(map[string]bool, len(p.Rules))
	for _, rule := range p.Rules {
		if rule.ID == "" || rule.ID != strings.TrimSpace(rule.ID) || seen[rule.ID] {
			return fmt.Errorf("bash rule id %q must be unique and canonical", rule.ID)
		}
		seen[rule.ID] = true
		if !rule.Action.Valid() || strings.TrimSpace(rule.Match.Value) == "" {
			return fmt.Errorf("bash rule %q needs a valid action and non-empty match", rule.ID)
		}
		if rule.Match.Type != "exact" && rule.Match.Type != "prefix" {
			return fmt.Errorf("bash rule %q has an invalid match type", rule.ID)
		}
	}
	return nil
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
