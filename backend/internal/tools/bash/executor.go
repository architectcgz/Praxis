package bash

import (
	toolmodel "praxis/internal/core/tool_invocation"
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
	"strings"
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

// Execute 在授权工作目录中运行 Bash，并将 stdout/stderr 合并为有界结果。
func Execute(ctx context.Context, call toolcontracts.AuthorizedToolCall) (toolcontracts.ToolResult, error) {
	if ctx == nil {
		return toolcontracts.ToolResult{}, errors.New("tool task context is required")
	}
	if err := ctx.Err(); err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if call.Name != toolcontracts.ToolBash {
		return toolcontracts.ToolResult{}, errors.New("bash executor does not support the call")
	}
	if strings.TrimSpace(call.Path) == "" || !authorizedDirectory(call.Path, call.WriteScopes) {
		return toolcontracts.ToolResult{}, errors.New("bash working directory is outside an authorized write scope")
	}
	shell, err := resolveShell(call.AllowedExecutables)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	var toolArguments arguments
	if err := json.Unmarshal(call.NormalizedArguments, &toolArguments); err != nil ||
		strings.TrimSpace(toolArguments.Command) == "" || len(toolArguments.Command) > maxCommandBytes ||
		toolArguments.Path != call.Path || toolArguments.Timeout <= 0 || toolArguments.Timeout > maxTimeout {
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

func resolveShell(allowed []string) (string, error) {
	for _, candidate := range allowed {
		candidate = strings.TrimSpace(candidate)
		base := strings.ToLower(filepath.Base(candidate))
		if base != "bash" && base != "bash.exe" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		if runtime.GOOS == "windows" && strings.EqualFold(candidate, "bash") {
			if programFiles := os.Getenv("ProgramFiles"); filepath.IsAbs(programFiles) {
				gitBash := filepath.Join(programFiles, "Git", "bin", "bash.exe")
				if path, err := exec.LookPath(gitBash); err == nil {
					return path, nil
				}
			}
		}
	}
	return "", errors.New("bash is not an allowed or available executable")
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

func authorizedDirectory(path string, scopes []string) bool {
	cleanPath := filepath.Clean(path)
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil || !pathutil.SamePath(cleanPath, realPath) {
		return false
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.IsDir() {
		return false
	}
	for _, scope := range scopes {
		cleanScope := filepath.Clean(scope)
		if !pathutil.IsWithin(cleanScope, cleanPath) {
			continue
		}
		realScope, err := filepath.EvalSymlinks(cleanScope)
		if err == nil && pathutil.SamePath(cleanScope, realScope) && pathutil.IsWithin(realScope, realPath) {
			return true
		}
	}
	return false
}
