//go:build windows

package daemon

import (
	"os"
	"os/exec"
)

// StartBackground 以后台进程方式启动 sunad。
// Windows 不需要 Unix 进程组设置，直接 Start 即可脱离当前 TUI 流程。
func StartBackground(cmd *exec.Cmd) error {
	return cmd.Start()
}

// StopRunning 停止当前 PID 文件指向的 daemon 进程。
// Windows 没有 SIGTERM 语义，这里使用 Kill 保持 stop 命令行为明确。
func StopRunning() error {
	pid, err := ReadPID()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
