package turn_test

import (
	"testing"
	"time"

	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"
)

func TestTurnIDIsDerivedFromTaskAndSequence(t *testing.T) {
	id, err := turnmodel.NewTurnID("task-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if id != "turn:task-1:2" {
		t.Fatalf("身份必须可由 Task 与序号重建: %s", id)
	}
	if _, err := turnmodel.NewTurnID("task-1", 0); err == nil {
		t.Fatal("序号必须从 1 开始")
	}
	if _, err := turnmodel.NewTurnID("", 1); err == nil {
		t.Fatal("缺少 Task 身份必须被拒绝")
	}
}

func TestTurnLifecycleRejectsInvalidSettlement(t *testing.T) {
	at := time.Now().UTC()
	id, err := turnmodel.NewTurnID("task-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	running, err := turnmodel.NewRunning(id, "task-1", "session-1", "agent-1", 1, at)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != turnmodel.TurnRunning || !running.EndedAt.IsZero() {
		t.Fatalf("新建迭代必须是 running 且未结算: %+v", running)
	}
	cases := []struct {
		name    string
		outcome turnmodel.TurnOutcome
		code    contracts.TaskFailureCode
		message string
		endedAt time.Time
	}{
		{"完成不能带失败", turnmodel.TurnCompleted, contracts.TaskFailureProvider, "", at.Add(time.Second)},
		{"失败必须带失败码", turnmodel.TurnFailed, "", "", at.Add(time.Second)},
		{"中断不能带详情", turnmodel.TurnInterrupted, contracts.TaskFailureInterrupted, "详情", at.Add(time.Second)},
		{"未知结果被拒绝", turnmodel.TurnOutcome("unknown"), contracts.TaskFailureTool, "", at.Add(time.Second)},
		{"结算早于创建被拒绝", turnmodel.TurnCompleted, "", "", at.Add(-time.Second)},
		{"零值结算时间被拒绝", turnmodel.TurnCompleted, "", "", time.Time{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			value := running
			endErr := value.End(testCase.outcome, testCase.code, testCase.message, testCase.endedAt)
			if endErr == nil {
				t.Fatalf("非法结算必须失败: %+v", value)
			}
			if value.Status != turnmodel.TurnRunning {
				t.Fatal("失败不能修改原对象")
			}
		})
	}
	ended := running
	if err := ended.End(turnmodel.TurnFailed, contracts.TaskFailureProvider, "provider 返回 500", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if ended.Status != turnmodel.TurnEnded || ended.EndedAt.IsZero() {
		t.Fatalf("结算后必须是终态: %+v", ended)
	}
	// 终态不可重写，恢复或重放都不得改变已提交结果。
	if err := ended.End(turnmodel.TurnCompleted, "", "", at.Add(2*time.Second)); err == nil {
		t.Fatal("重复结算必须失败")
	}
	if err := ended.Validate(); err != nil {
		t.Fatalf("终态必须自校验通过: %v", err)
	}
	if _, err := turnmodel.NewRunning("turn:", "task-1", "session-1", "agent-1", 1, at); err == nil {
		t.Fatal("非 canonical 身份必须被拒绝")
	}
	if _, err := turnmodel.NewRunning(id, "task-1", "session-1", "agent-1", 1, time.Time{}); err == nil {
		t.Fatal("缺少创建时间必须被拒绝")
	}
	if !turnmodel.TurnRunning.Valid() || !turnmodel.TurnEnded.Valid() ||
		turnmodel.TurnStatus("unknown").Valid() {
		t.Fatal("状态校验必须只接受已支持取值")
	}
}
