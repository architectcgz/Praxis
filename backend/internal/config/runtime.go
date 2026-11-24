package config

import "time"

// Runtime 是装配时确定的进程级运行策略，不随会话重载改变。
type Runtime struct {
	Model     ModelRuntime
	Lifecycle Lifecycle
	Execution ExecutionLimits
}

type ModelRuntime struct {
	ConnectTimeout        time.Duration
	ConnectionKeepAlive   time.Duration
	IdleConnectionTimeout time.Duration
	ResponseHeaderTimeout time.Duration
	StreamIdleTimeout     time.Duration
	RequestTimeout        time.Duration
	MaxAttempts           int
}

type Lifecycle struct {
	StartupWait time.Duration
	Preparation time.Duration
	Commit      time.Duration
	Settlement  time.Duration
	Transition  time.Duration
	Shutdown    time.Duration
}

type ExecutionLimits struct {
	ReadOnlyParallelism int
	ActiveSubagents     int
}

func DefaultRuntime() Runtime {
	return Runtime{
		Model: ModelRuntime{
			ConnectTimeout:        10 * time.Second,
			ConnectionKeepAlive:   30 * time.Second,
			IdleConnectionTimeout: 90 * time.Second,
			ResponseHeaderTimeout: time.Minute,
			StreamIdleTimeout:     time.Minute,
			RequestTimeout:        10 * time.Minute,
			MaxAttempts:           4,
		},
		Lifecycle: Lifecycle{
			StartupWait: 30 * time.Second,
			Preparation: 30 * time.Second,
			Commit:      30 * time.Second,
			Settlement:  30 * time.Second,
			Transition:  30 * time.Second,
			Shutdown:    30 * time.Second,
		},
		Execution: ExecutionLimits{
			ReadOnlyParallelism: 8,
			ActiveSubagents:     10,
		},
	}
}
