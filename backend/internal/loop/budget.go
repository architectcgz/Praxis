package loop

import (
	"praxis/internal/contracts"
	"praxis/internal/core/model"
)

// taskBudget 检查单次模型输入和累计输出，工具次数由 Executor 在准入事务中检查。
type taskBudget struct {
	limits      contracts.ResourceLimits
	outputBytes int64
}

func newTaskBudget(limits contracts.ResourceLimits) *taskBudget { return &taskBudget{limits: limits} }

func (b *taskBudget) reserveInput(request model.ModelRequest) error {
	if b.limits.MaxInputBytes > 0 && stepInputBytes(request) > b.limits.MaxInputBytes {
		return fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task input limit exceeded")
	}
	return nil
}

func (b *taskBudget) addOutput(bytes int) error {
	b.outputBytes += int64(bytes)
	if b.limits.MaxOutputBytes > 0 && b.outputBytes > b.limits.MaxOutputBytes {
		return fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task output limit exceeded")
	}
	return nil
}

func stepInputBytes(request model.ModelRequest) int64 {
	var total int64
	total += int64(len(request.Context.SystemPrompt))
	for _, entry := range request.Context.Entries {
		for _, block := range entry.Content {
			total += int64(len(block.Text))
			total += int64(len(block.Input))
			if block.ReasoningState != nil {
				total += int64(len(block.ReasoningState.Payload))
			}
		}
	}
	return total
}
