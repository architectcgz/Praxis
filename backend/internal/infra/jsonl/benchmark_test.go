package jsonl

import (
	"context"
	"fmt"
	"praxis/internal/contracts"
	"testing"
	"time"

	taskmodel "praxis/internal/core/task"
)

func seedHistory(b *testing.B, f *storeFixture, count int) {
	b.Helper()
	// 只在准备阶段扩大引用缓存，测量时恢复生产缓存上限。
	f.store.messages = newMessageCache(count + 1)
	requireOK(b, f.store.InTx(context.Background(), func(ctx context.Context) error {
		for i := 0; i < count; i++ {
			if _, err := f.repos.Messages.Append(ctx, f.agent, userMessage(fmt.Sprintf("history-%d", i))); err != nil {
				return err
			}
		}
		return nil
	}))
	f.store.messages = newMessageCache(4096)
}

func BenchmarkTaskProgress(b *testing.B) {
	for _, count := range []int{0, 1000, 10000} {
		b.Run(fmt.Sprintf("history_%d", count), func(b *testing.B) {
			f := newStoreFixture(b)
			task := f.activeTask(b)
			seedHistory(b, f, count)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				requireOK(b, f.store.InTx(ctx, func(txCtx context.Context) error {
					current, err := f.repos.Tasks.Get(txCtx, task.ID)
					if err != nil {
						return err
					}
					current.Progress.OutputBytes++
					return f.repos.Tasks.Save(txCtx, current)
				}))
			}
		})
	}
}

func BenchmarkTaskGet(b *testing.B) {
	f := newStoreFixture(b)
	task := f.activeTask(b)
	seedHistory(b, f, 10000)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := f.repos.Tasks.Get(ctx, task.ID)
		requireOK(b, err)
	}
}

func BenchmarkSessionMessages(b *testing.B) {
	for _, hot := range []bool{false, true} {
		b.Run(fmt.Sprintf("hot_%v", hot), func(b *testing.B) {
			f := newStoreFixture(b)
			seedHistory(b, f, 1000)
			ctx := context.Background()
			_, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
			requireOK(b, err)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !hot {
					f.store.messages = newMessageCache(4096)
				}
				_, err := f.repos.Messages.List(ctx, f.agent, 990, 10)
				requireOK(b, err)
			}
		})
	}
}

func BenchmarkPendingHead(b *testing.B) {
	for _, count := range []int{0, 1000} {
		b.Run(fmt.Sprintf("ended_%d", count), func(b *testing.B) {
			f := newStoreFixture(b)
			ctx := context.Background()
			for start := 0; start < count; start += 50 {
				requireOK(b, f.store.InTx(ctx, func(txCtx context.Context) error {
					for i := start; i < min(start+50, count); i++ {
						id := fmt.Sprint("ended-", i)
						task, err := taskmodel.NewTask(contracts.TaskID(id), "session", "agent", contracts.RequestID(id), "input:"+id, f.config(), taskmodel.ContextSelection{}, time.Now())
						if err != nil {
							return err
						}
						if err := task.MarkRunning(time.Now()); err != nil {
							return err
						}
						if err := task.End(taskmodel.TaskCompleted, "", time.Now()); err != nil {
							return err
						}
						if err := f.repos.Tasks.Save(txCtx, task); err != nil {
							return err
						}
					}
					return nil
				}))
			}
			requireOK(b, f.store.InTx(ctx, func(txCtx context.Context) error {
				task, err := taskmodel.NewPendingTask("pending", "session", "agent", "pending", "input:pending", 1, time.Now())
				if err != nil {
					return err
				}
				return f.repos.Tasks.Save(txCtx, task)
			}))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := f.repos.Tasks.FindNextPendingByAgent(ctx, "agent")
				requireOK(b, err)
			}
		})
	}
}

func BenchmarkReplay(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("messages_%d", count), func(b *testing.B) {
			f := newStoreFixture(b)
			seedHistory(b, f, count)
			requireOK(b, f.store.Close(context.Background()))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				store, err := Open(context.Background(), f.root)
				requireOK(b, err)
				requireOK(b, store.Close(context.Background()))
			}
		})
	}
}
