package security

import "testing"

func TestCommandPolicyPrecedenceAndExactMatching(t *testing.T) {
	rules := []CommandRule{
		{ID: "allow", Action: CommandAllow, Match: CommandMatch{Type: "exact", Value: "git push --force origin main"}},
		{ID: "ask", Action: CommandAsk, Match: CommandMatch{Type: "prefix", Value: "git push "}},
		{ID: "deny", Action: CommandDeny, Match: CommandMatch{Type: "prefix", Value: "git push --force"}},
	}
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		policy := CommandPolicy{DefaultAction: CommandAllow}
		for _, index := range order {
			policy.Rules = append(policy.Rules, rules[index])
		}
		if got := policy.Decide("git push --force origin main"); got != (CommandDecision{Action: CommandDeny, RuleID: "deny"}) {
			t.Fatalf("优先级随配置顺序改变：%+v", got)
		}
		if got := policy.Decide("git push origin main"); got.Action != CommandAsk {
			t.Fatalf("ask 未覆盖 default allow：%+v", got)
		}
	}
	policy := CommandPolicy{DefaultAction: CommandAsk, Rules: []CommandRule{
		{ID: "status", Action: CommandAllow, Match: CommandMatch{Type: "exact", Value: "git status"}},
	}}
	for _, command := range []string{"git status; rm -rf build", "git status && pwd", "git status | cat", "git status $(pwd)", "git status\npwd", " git status", "git status "} {
		if got := policy.Decide(command); got.Action != CommandAsk {
			t.Errorf("拼接或不同命令被自动放行 %q：%+v", command, got)
		}
	}
	if got := policy.Decide("git status"); got.Action != CommandAllow {
		t.Fatal(got)
	}
}

func TestCommandPolicyValidation(t *testing.T) {
	valid := CommandRule{ID: "rule", Action: CommandAllow, Match: CommandMatch{Type: "exact", Value: "git status"}}
	for _, policy := range []CommandPolicy{
		{DefaultAction: "unknown"},
		{DefaultAction: CommandAsk, Rules: []CommandRule{valid, valid}},
		{DefaultAction: CommandAsk, Rules: []CommandRule{{ID: "", Action: CommandAsk, Match: valid.Match}}},
		{DefaultAction: CommandAsk, Rules: []CommandRule{{ID: "rule", Action: "unknown", Match: valid.Match}}},
		{DefaultAction: CommandAsk, Rules: []CommandRule{{ID: "rule", Action: CommandAsk, Match: CommandMatch{Type: "exact", Value: " "}}}},
		{DefaultAction: CommandAsk, Rules: []CommandRule{{ID: "rule", Action: CommandAllow, Match: CommandMatch{Type: "prefix", Value: "git "}}}},
		{DefaultAction: CommandAsk, Rules: []CommandRule{{ID: "rule", Action: CommandAsk, Match: CommandMatch{Type: "glob", Value: "git *"}}}},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatalf("非法配置被接受：%+v", policy)
		}
	}
}
