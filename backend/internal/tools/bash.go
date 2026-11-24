package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"praxis/internal/pathutil"
	"strings"
	"time"
)

const bashInputSchema = `{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "minLength": 1,
      "maxLength": %[1]d,
      "description": "要执行的 Bash 命令或多行脚本；maxLength 按字符计，实际 UTF-8 输入不得超过 %[1]d 字节"
    },
    "path": {
      "type": "string",
      "description": "脚本工作目录，可以是绝对路径或相对于项目根目录的路径"
    },
    "timeout": {
      "type": "number",
      "exclusiveMinimum": 0,
      "maximum": %[2]d,
      "description": "超时时间，单位为秒；省略时为 %[3]d 秒，最长 %[2]d 秒"
    }
  },
  "required": ["command"],
  "additionalProperties": false
}`

type BashLimits struct {
	DefaultTimeoutSeconds int
	MaxTimeoutSeconds     int
	MaxCommandBytes       int
	ExitWait              time.Duration
	InlineResultBytes     int
}

type bashArguments struct {
	Command string   `json:"command"`
	Path    string   `json:"path"`
	Timeout *float64 `json:"timeout"`
}

func (a *bashArguments) PathArgument() *string { return &a.Path }

func NewBash(shell string, limits BashLimits) Tool {
	return Function[bashArguments]{
		Name: "bash",
		Description: "在已授权的项目目录内执行 Bash 命令或多行脚本，返回合并后的 stdout 和 stderr。" +
			"输入 command，可选 path 和 timeout（秒）；相对 path 按项目根目录解析。" +
			"脚本在宿主机执行；只在用户显式授予命令权限时可用。" +
			fmt.Sprintf("输出或编码结果超过 %d 字节时，将完整正文写入当前会话的临时文件，返回 outputFile、size 和执行状态，可用 read_file 分段读取。", limits.InlineResultBytes) +
			"脚本受当前执行的取消和超时控制；不适合启动长期进程。",
		InputSchema: json.RawMessage(fmt.Sprintf(bashInputSchema, limits.MaxCommandBytes, limits.MaxTimeoutSeconds, limits.DefaultTimeoutSeconds)),

		DefaultArguments: func() bashArguments {
			return bashArguments{Path: ".", Timeout: new(float64(limits.DefaultTimeoutSeconds))}
		},
		PrepareArguments: func(args bashArguments) (bashArguments, error) {
			if strings.TrimSpace(args.Command) == "" {
				return args, errors.New("bash arguments are invalid: command is required")
			}
			if len(args.Command) > limits.MaxCommandBytes {
				return args, fmt.Errorf("bash arguments are invalid: command must not exceed %d UTF-8 bytes", limits.MaxCommandBytes)
			}
			if args.Timeout == nil || math.IsNaN(*args.Timeout) || math.IsInf(*args.Timeout, 0) ||
				*args.Timeout <= 0 || *args.Timeout > float64(limits.MaxTimeoutSeconds) {
				return args, fmt.Errorf("bash arguments are invalid: timeout must be greater than 0 and at most %d seconds", limits.MaxTimeoutSeconds)
			}
			return args, nil
		},
		Execute: func(ctx context.Context, args bashArguments, env Environment) (ToolResult, error) {
			return runBash(ctx, args, env, shell, limits)
		},
	}
}

type bashOutput struct {
	Output     string `json:"output,omitempty"`
	OutputFile string `json:"outputFile,omitempty"`
	Size       int64  `json:"size,omitzero"`
	Message    string `json:"message,omitempty"`
	ExitCode   *int   `json:"exitCode"`
	TimedOut   bool   `json:"timedOut,omitzero"`
}

// runBash 在项目目录内运行 Bash，超过内联限制的 stdout/stderr 合并后完整落盘。
func runBash(ctx context.Context, toolArguments bashArguments, environment Environment, shell string, limits BashLimits) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	if shell == "" {
		return ToolResult{}, errors.New("bash executable is not available")
	}
	// 路径已在装配时选定；实际执行前仍检查普通文件，由操作系统检查执行权限。
	if info, err := os.Stat(shell); err != nil || !info.Mode().IsRegular() {
		return ToolResult{}, errors.New("bash executable is not available")
	}
	if !bashProjectDirectory(toolArguments.Path, environment.ProjectPath) {
		return ToolResult{}, errors.New("bash working directory is outside the project")
	}

	runContext, cancel := context.WithTimeout(ctx, time.Duration(*toolArguments.Timeout*float64(time.Second)))
	defer cancel()

	command := exec.CommandContext(runContext, shell, "--noprofile", "--norc", "-c", toolArguments.Command)
	command.Dir = toolArguments.Path
	command.WaitDelay = limits.ExitWait
	outputBuffer := newOutputWriter(environment.OutputDirectory, limits.InlineResultBytes)
	command.Stdout = outputBuffer
	command.Stderr = outputBuffer
	err := command.Run()
	content, file, saveErr := outputBuffer.Finish()
	if saveErr != nil {
		result := NewToolError("tool_output_save_failed", "The command has already run, but saving its output failed: "+saveErr.Error()+". Do not rerun the command solely to recover its output.")
		result.SideEffect = true
		return result, nil
	}
	if ctx.Err() != nil {
		return ToolResult{}, ctx.Err()
	}
	toolOutput := bashOutput{
		Output:     content,
		OutputFile: file.Path,
		Size:       file.Size,
		TimedOut:   errors.Is(runContext.Err(), context.DeadlineExceeded),
	}
	if file.Path != "" {
		toolOutput.Message = "完整输出已保存到临时文件，请使用 read_file 按 offset 和 limit 分段读取；临时文件可能被系统清理。"
	}
	if !toolOutput.TimedOut {
		if err != nil {
			exitError, ok := errors.AsType[*exec.ExitError](err)
			if !ok {
				return ToolResult{}, fmt.Errorf("start bash: %w", err)
			}
			toolOutput.ExitCode = new(exitError.ExitCode())
		} else {
			toolOutput.ExitCode = new(0)
		}
	}
	encoded, err := encodeBashOutput(toolOutput, environment.OutputDirectory, limits.InlineResultBytes)
	if err != nil {
		result := NewToolError("tool_output_save_failed", "The command has already run, but saving its result failed: "+err.Error()+". Do not rerun the command solely to recover its output.")
		result.SideEffect = true
		return result, nil
	}
	result := NewToolSuccess(string(encoded), false)
	result.SideEffect = true
	return result, nil
}

func encodeBashOutput(toolOutput bashOutput, directory string, inlineBytes int) ([]byte, error) {
	encoded, err := json.Marshal(toolOutput)
	if err != nil {
		return nil, err
	}
	if len(encoded) <= inlineBytes || toolOutput.OutputFile != "" {
		return encoded, nil
	}
	// JSON 转义也可能使小正文超过内联上限，此时仍保存完整正文而不是截断。
	file, err := saveOutput(directory, toolOutput.Output)
	if err != nil {
		return nil, err
	}
	toolOutput.Output = ""
	toolOutput.OutputFile, toolOutput.Size = file.Path, file.Size
	toolOutput.Message = "完整输出已保存到临时文件，请使用 read_file 按 offset 和 limit 分段读取；临时文件可能被系统清理。"
	return json.Marshal(toolOutput)
}

// projectDirectory 拒绝项目外目录和符号链接路径；这不是 Bash 进程隔离。
func bashProjectDirectory(path, projectRoot string) bool {
	if !pathutil.IsWithin(projectRoot, path) {
		return false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil || !pathutil.SamePath(path, realPath) {
		return false
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.IsDir() {
		return false
	}
	realRoot, err := filepath.EvalSymlinks(projectRoot)
	return err == nil && pathutil.SamePath(projectRoot, realRoot) && pathutil.IsWithin(realRoot, realPath)
}
