package bindings

import (
	"os/exec"
	"syscall"
)

func revealFileCommand(path string) (*exec.Cmd, error) {
	command := exec.Command("explorer.exe")
	// Explorer 使用自己的参数解析规则，只引用路径，避免含空格目录导致 /select 失效。
	command.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: "explorer.exe /select," + syscall.EscapeArg(path),
	}
	return command, nil
}
