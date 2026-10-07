package loop

import (
	"praxis/internal/contracts"
	"praxis/internal/core/model"
)

const defaultMaxToolCalls = 128

// taskBudget 执行单次 task 的资源限制。
// 输出字节数跨迭代的模型输出和工具结果累计，工具计数仅在调用成功后递增。
type taskBudget struct {
	limits       contracts.ResourceLimits
	maxToolCalls int
	outputBytes  int64
	toolCalls    int
}

func newTaskBudget(limits contracts.ResourceLimits) *taskBudget {
	budget := &taskBudget{
		limits:       limits,
		maxToolCalls: limits.MaxToolCalls,
	}
	if budget.maxToolCalls <= 0 {
		budget.maxToolCalls = defaultMaxToolCalls
	}
	return budget
}

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

func (b *taskBudget) reserveToolCalls(count int) error {
	if b.toolCalls+count > b.maxToolCalls {
		return fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task tool call limit exceeded")
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
		}
	}
	return total
}
