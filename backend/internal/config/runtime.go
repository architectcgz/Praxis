package config

import (
	"fmt"
	"praxis/internal/logging"
	"strings"
	"time"
)

// Runtime 是装配时确定的进程级运行策略，不随会话重载改变。
type Runtime struct {
	Model     ModelRuntime
	Lifecycle Lifecycle
	Execution ExecutionLimits
	Context   ContextRuntime
	Review    ReviewRuntime
	Tools     ToolRuntime
	Pages     PageRuntime
}

type ModelRuntime struct {
	ConnectTimeout        time.Duration
	ConnectionKeepAlive   time.Duration
	IdleConnectionTimeout time.Duration
	ResponseHeaderTimeout time.Duration
	StreamIdleTimeout     time.Duration
	RequestTimeout        time.Duration
	MaxAttempts           int
	Stream                ProviderStreamLimits
}

// ProviderStreamLimits 是进程内部资源上限，不从用户配置加载。
type ProviderStreamLimits struct {
	MaxFrameBytes       int
	EventBufferCapacity int
	MaxErrorBodyBytes   int
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

type ContextRuntime struct {
	NearLimitPercent     int
	SummaryTargetPercent int
	MaxSummaryTokens     int
	MinMarginTokens      int
	MarginPercent        int
	RequestReserveTokens int
	SummaryTimeout       time.Duration
	ToolResultOverhead   int
	KeepToolResultTokens int
}

type ReviewRuntime struct {
	Timeout           time.Duration
	MaxTextBytes      int
	TargetReasonChars int // 提示词与 Schema 说明中的理由长度目标。
	MaxReasonBytes    int // 接纳理由时按 UTF-8 字节数执行的硬上限。
}

type ToolRuntime struct {
	Bash              BashRuntime
	ReadFile          ReadFileRuntime
	MaxPatchBytes     int
	InlineResultBytes int // 工具结果编码后的内联字节上限，与原文读取上限独立。
}

type BashRuntime struct {
	DefaultTimeoutSeconds int
	MaxTimeoutSeconds     int
	MaxCommandBytes       int
	ExitWait              time.Duration
}

type ReadFileRuntime struct {
	DefaultReadBytes int
	MaxReadBytes     int
}

const maxHistoryPageSize = 200

type PageRuntime struct {
	DefaultHistorySize int
	MaxHistorySize     int
	ListLimit          int
}

// ParseLogLevel 将未设置或空白值解释为 info；非法的非空值阻止启动。
func ParseLogLevel(value string) (logging.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return logging.InfoLevel, nil
	case "debug":
		return logging.DebugLevel, nil
	case "warn", "warning":
		return logging.WarnLevel, nil
	case "error":
		return logging.ErrorLevel, nil
	default:
		return 0, fmt.Errorf("invalid PRAXIS_LOG_LEVEL %q: expected debug, info, warn, or error", value)
	}
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
			Stream: ProviderStreamLimits{
				MaxFrameBytes:       2 << 20,
				EventBufferCapacity: 16,
				MaxErrorBodyBytes:   16 << 10,
			},
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
		Context: ContextRuntime{
			NearLimitPercent:     90,
			SummaryTargetPercent: 60,
			MaxSummaryTokens:     4096,
			MinMarginTokens:      256,
			MarginPercent:        2,
			RequestReserveTokens: 128,
			SummaryTimeout:       2 * time.Minute,
			ToolResultOverhead:   32,
			KeepToolResultTokens: 256,
		},
		Review: ReviewRuntime{
			Timeout:           time.Minute,
			MaxTextBytes:      4096,
			TargetReasonChars: 200,
			MaxReasonBytes:    1000,
		},
		Tools: ToolRuntime{
			Bash: BashRuntime{
				DefaultTimeoutSeconds: 120,
				MaxTimeoutSeconds:     600,
				MaxCommandBytes:       128 * 1024,
				ExitWait:              time.Second,
			},
			ReadFile: ReadFileRuntime{
				DefaultReadBytes: 32 * 1024,
				MaxReadBytes:     50 * 1024,
			},
			MaxPatchBytes:     512 * 1024,
			InlineResultBytes: 50 * 1024,
		},
		Pages: PageRuntime{
			DefaultHistorySize: min(50, maxHistoryPageSize),
			MaxHistorySize:     maxHistoryPageSize,
			ListLimit:          100,
		},
	}
}
