//go:build !windows

package bindings

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

func revealFileCommand(path string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path), nil
	case "linux":
		return exec.Command("xdg-open", filepath.Dir(path)), nil
	default:
		return nil, fmt.Errorf("当前系统不支持打开文件所在位置：%s", runtime.GOOS)
	}
}
