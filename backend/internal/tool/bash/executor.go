package bash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"praxis/internal/pathutil"
	"praxis/internal/tool"
	tooloutput "praxis/internal/tool/output"
	"time"
)

type output struct {
	Output     string `json:"output,omitempty"`
	OutputFile string `json:"outputFile,omitempty"`
	Size       int64  `json:"size,omitzero"`
	Message    string `json:"message,omitempty"`
	ExitCode   *int   `json:"exitCode"`
	TimedOut   bool   `json:"timedOut,omitzero"`
}

// call 在项目目录内运行 Bash，超过内联限制的 stdout/stderr 合并后完整落盘。
func (t *bashTool) call(ctx context.Context, toolArguments arguments, environment tool.Environment) (tool.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	if t.shell == "" {
		return tool.ToolResult{}, errors.New("bash executable is not available")
	}
	// 路径已在装配时选定；实际执行前仍检查普通文件，由操作系统检查执行权限。
	if info, err := os.Stat(t.shell); err != nil || !info.Mode().IsRegular() {
		return tool.ToolResult{}, errors.New("bash executable is not available")
	}
	if !projectDirectory(toolArguments.Path, environment.ProjectPath) {
		return tool.ToolResult{}, errors.New("bash working directory is outside the project")
	}

	runContext, cancel := context.WithTimeout(ctx, time.Duration(*toolArguments.Timeout*float64(time.Second)))
	defer cancel()

	command := exec.CommandContext(runContext, t.shell, "--noprofile", "--norc", "-c", toolArguments.Command)
	command.Dir = toolArguments.Path
	command.WaitDelay = t.limits.ExitWait
	outputBuffer := tooloutput.NewWriter(environment.OutputDirectory, t.limits.InlineResultBytes)
	command.Stdout = outputBuffer
	command.Stderr = outputBuffer
	err := command.Run()
	content, file, saveErr := outputBuffer.Finish()
	if saveErr != nil {
		result := tool.NewToolError("tool_output_save_failed", "The command has already run, but saving its output failed: "+saveErr.Error()+". Do not rerun the command solely to recover its output.")
		result.SideEffect = true
		return result, nil
	}
	if ctx.Err() != nil {
		return tool.ToolResult{}, ctx.Err()
	}
	toolOutput := output{
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
				return tool.ToolResult{}, fmt.Errorf("start bash: %w", err)
			}
			toolOutput.ExitCode = new(exitError.ExitCode())
		} else {
			toolOutput.ExitCode = new(0)
		}
	}
	encoded, err := encodeOutput(toolOutput, environment.OutputDirectory, t.limits.InlineResultBytes)
	if err != nil {
		result := tool.NewToolError("tool_output_save_failed", "The command has already run, but saving its result failed: "+err.Error()+". Do not rerun the command solely to recover its output.")
		result.SideEffect = true
		return result, nil
	}
	result := tool.NewToolSuccess(string(encoded), false)
	result.SideEffect = true
	return result, nil
}

func encodeOutput(toolOutput output, directory string, inlineBytes int) ([]byte, error) {
	encoded, err := json.Marshal(toolOutput)
	if err != nil {
		return nil, err
	}
	if len(encoded) <= inlineBytes || toolOutput.OutputFile != "" {
		return encoded, nil
	}
	// JSON 转义也可能使小正文超过内联上限，此时仍保存完整正文而不是截断。
	file, err := tooloutput.Save(directory, toolOutput.Output)
	if err != nil {
		return nil, err
	}
	toolOutput.Output = ""
	toolOutput.OutputFile, toolOutput.Size = file.Path, file.Size
	toolOutput.Message = "完整输出已保存到临时文件，请使用 read_file 按 offset 和 limit 分段读取；临时文件可能被系统清理。"
	return json.Marshal(toolOutput)
}

// projectDirectory 拒绝项目外目录和符号链接路径；这不是 Bash 进程隔离。
func projectDirectory(path, projectRoot string) bool {
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
