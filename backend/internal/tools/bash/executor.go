package bash

import (
	coretool "praxis/internal/core/tool"
	toolmodel "praxis/internal/core/tool_execution"
	toolcontracts "praxis/internal/tools/contracts"
	"praxis/internal/utils/pathutil"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type output struct {
	Output    string `json:"output"`
	ExitCode  *int   `json:"exitCode"`
	TimedOut  bool   `json:"timedOut,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

type outputWriter struct {
	buffer    bytes.Buffer
	truncated bool
}

func (w *outputWriter) Write(value []byte) (int, error) {
	remaining := toolmodel.MaxInlineToolResultBytes - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(value), nil
	}
	if len(value) > remaining {
		_, _ = w.buffer.Write(value[:remaining])
		w.truncated = true
		return len(value), nil
	}
	_, _ = w.buffer.Write(value)
	return len(value), nil
}

// Execute 在 Workspace 内的工作目录中运行 Bash，并将 stdout/stderr 合并为有界结果。
func Execute(ctx context.Context, call toolcontracts.ExecutionCall) (toolcontracts.ToolResult, error) {
	if ctx == nil {
		return toolcontracts.ToolResult{}, errors.New("tool task context is required")
	}
	if err := ctx.Err(); err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if call.Name != coretool.ToolBash {
		return toolcontracts.ToolResult{}, errors.New("bash executor does not support the call")
	}
	if !workspaceDirectory(call.Path, call.WorkspacePath) {
		return toolcontracts.ToolResult{}, errors.New("bash working directory is outside the workspace")
	}
	shell, err := resolveShell()
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	var toolArguments arguments
	if err := json.Unmarshal(call.NormalizedArguments, &toolArguments); err != nil {
		return toolcontracts.ToolResult{}, fmt.Errorf("decode normalized bash arguments: %w", err)
	}
	if toolArguments.Path != call.Path {
		return toolcontracts.ToolResult{}, errors.New("normalized bash arguments are invalid")
	}

	runContext, cancel := context.WithTimeout(ctx, time.Duration(toolArguments.Timeout*float64(time.Second)))
	defer cancel()

	command := exec.CommandContext(runContext, shell, "--noprofile", "--norc", "-c", toolArguments.Command)
	command.Dir = toolArguments.Path
	command.WaitDelay = time.Second
	var outputBuffer outputWriter
	command.Stdout = &outputBuffer
	command.Stderr = &outputBuffer
	err = command.Run()
	if ctx.Err() != nil {
		return toolcontracts.ToolResult{}, ctx.Err()
	}
	toolOutput := output{
		Output:    outputBuffer.buffer.String(),
		TimedOut:  errors.Is(runContext.Err(), context.DeadlineExceeded),
		Truncated: outputBuffer.truncated,
	}
	if !toolOutput.TimedOut {
		if err != nil {
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) {
				return toolcontracts.ToolResult{}, fmt.Errorf("start bash: %w", err)
			}
			code := exitError.ExitCode()
			toolOutput.ExitCode = &code
		} else {
			code := 0
			toolOutput.ExitCode = &code
		}
	}
	encoded, truncated, err := encodeOutput(toolOutput)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	result := toolcontracts.NewToolSuccess(string(encoded), truncated)
	result.SideEffect = true
	return result, nil
}

func resolveShell() (string, error) {
	if path, err := exec.LookPath("bash"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		if programFiles := os.Getenv("ProgramFiles"); filepath.IsAbs(programFiles) {
			gitBash := filepath.Join(programFiles, "Git", "bin", "bash.exe")
			if path, err := exec.LookPath(gitBash); err == nil {
				return path, nil
			}
		}
	}
	return "", errors.New("bash executable is not available")
}

func encodeOutput(toolOutput output) ([]byte, bool, error) {
	encoded, err := json.Marshal(toolOutput)
	if err != nil {
		return nil, false, errors.New("bash could not encode its result")
	}
	if len(encoded) <= toolmodel.MaxInlineToolResultBytes {
		return encoded, toolOutput.Truncated, nil
	}

	raw := []byte(toolOutput.Output)
	low, high := 0, len(raw)
	best := []byte(nil)
	for low <= high {
		middle := low + (high-low)/2
		candidate := toolOutput
		candidate.Output = string(raw[:middle])
		candidate.Truncated = true
		encodedCandidate, marshalErr := json.Marshal(candidate)
		if marshalErr != nil {
			return nil, false, errors.New("bash could not encode its bounded result")
		}
		if len(encodedCandidate) <= toolmodel.MaxInlineToolResultBytes {
			best = encodedCandidate
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	if best == nil {
		return nil, false, errors.New("bash result metadata exceeds the size limit")
	}
	return best, true, nil
}

// workspaceDirectory 拒绝工作区外目录和符号链接路径；这不是 Bash 进程隔离。
func workspaceDirectory(path, workspace string) bool {
	if !pathutil.IsAbsoluteNormalized(path) || !pathutil.IsAbsoluteNormalized(workspace) || !pathutil.IsWithin(workspace, path) {
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
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	return err == nil && pathutil.SamePath(workspace, realWorkspace) && pathutil.IsWithin(realWorkspace, realPath)
}
