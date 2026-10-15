// Package security 定义 Bash 命令的审核策略，不提供进程或文件系统隔离。
package security

import (
	"fmt"
	"slices"
	"strings"
)

type CommandAction string

const (
	CommandAllow CommandAction = "allow"
	CommandAsk   CommandAction = "ask"
	CommandDeny  CommandAction = "deny"
)

func (a CommandAction) Valid() bool {
	return a == CommandAllow || a == CommandAsk || a == CommandDeny
}

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
	ID     string        `json:"id"`
	Action CommandAction `json:"action"`
	Match  CommandMatch  `json:"match"`
}

type CommandPolicy struct {
	DefaultAction CommandAction `json:"defaultAction"`
	Rules         []CommandRule `json:"rules"`
}

type CommandDecision struct {
	Action CommandAction
	RuleID string
}

// Validate 只读校验完整配置；匹配值保留空格和换行，以最终命令原文进行比较。
func (p CommandPolicy) Validate() error {
	if !p.DefaultAction.Valid() {
		return fmt.Errorf("invalid bash defaultAction %q", p.DefaultAction)
	}
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
		if rule.Action == CommandAllow && rule.Match.Type != "exact" {
			return fmt.Errorf("bash allow rule %q requires an exact match", rule.ID)
		}
	}
	return nil
}

func (p CommandPolicy) Clone() CommandPolicy {
	p.Rules = slices.Clone(p.Rules)
	return p
}

// Decide 按 deny > ask > allow > default 决策，与配置排列顺序无关。
func (p CommandPolicy) Decide(command string) CommandDecision {
	for _, action := range []CommandAction{CommandDeny, CommandAsk, CommandAllow} {
		for _, rule := range p.Rules {
			if rule.Action == action && rule.Match.Matches(command) {
				return CommandDecision{Action: action, RuleID: rule.ID}
			}
		}
	}
	return CommandDecision{Action: p.DefaultAction, RuleID: "default"}
}
