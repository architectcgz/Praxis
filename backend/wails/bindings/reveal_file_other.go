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
		return nil, fmt.Errorf("revealing file locations is not supported on this operating system: %s", runtime.GOOS)
	}
}
