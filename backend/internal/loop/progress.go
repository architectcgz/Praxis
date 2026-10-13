package loop

import (
	"context"
	"praxis/internal/contracts"
)

func (r *Runner) recordRequest(ctx context.Context, id contracts.TaskID, sequence uint64) error {
	return r.Transactions.InTx(ctx, func(txCtx context.Context) error {
		task, err := r.Tasks.Get(txCtx, id)
		if err != nil {
			return err
		}
		task.Progress.NextSequence = sequence + 1
		task.Progress.ModelRequests++
		return r.Tasks.Save(txCtx, task)
	})
}
func (r *Runner) recordOutput(ctx context.Context, id contracts.TaskID, bytes int) error {
	return r.Transactions.InTx(context.WithoutCancel(ctx), func(txCtx context.Context) error {
		task, err := r.Tasks.Get(txCtx, id)
		if err != nil {
			return err
		}
		task.Progress.OutputBytes += int64(bytes)
		return r.Tasks.Save(txCtx, task)
	})
}
