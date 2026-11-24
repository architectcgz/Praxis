package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"praxis/internal/config"
	"praxis/internal/tools"
	"runtime"
)

func buildToolRegistry(policy config.ToolRuntime, extensions []tools.Tool) (*tools.ToolRegistry, error) {
	registry := tools.NewToolRegistry()
	builtins := []tools.Tool{
		tools.NewReadFile(tools.ReadFileLimits{
			DefaultReadBytes:  policy.ReadFile.DefaultReadBytes,
			MaxReadBytes:      policy.ReadFile.MaxReadBytes,
			InlineResultBytes: policy.InlineResultBytes,
		}),
		tools.NewBash(findBash(), tools.BashLimits{
			DefaultTimeoutSeconds: policy.Bash.DefaultTimeoutSeconds,
			MaxTimeoutSeconds:     policy.Bash.MaxTimeoutSeconds,
			MaxCommandBytes:       policy.Bash.MaxCommandBytes,
			ExitWait:              policy.Bash.ExitWait,
			InlineResultBytes:     policy.InlineResultBytes,
		}),
		tools.NewApplyPatch(policy.MaxPatchBytes),
	}
	for _, candidate := range append(builtins, extensions...) {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// findBash 只在装配时解析可执行路径；缺失不阻止启动，由实际调用报告。
func findBash() string {
	if path, err := exec.LookPath("bash"); err == nil {
		// 固定绝对路径，避免执行时被脚本工作目录重新解释。
		if absolute, err := filepath.Abs(path); err == nil {
			return absolute
		}
	}
	if runtime.GOOS == "windows" {
		if programFiles := os.Getenv("ProgramFiles"); filepath.IsAbs(programFiles) {
			if path, err := exec.LookPath(filepath.Join(programFiles, "Git", "bin", "bash.exe")); err == nil {
				return path
			}
		}
	}
	return ""
}
