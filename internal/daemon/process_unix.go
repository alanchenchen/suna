//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// StartBackground 以后台进程方式启动 sunad。
// Unix 平台需要设置独立进程组，避免 TUI 退出或收到终端信号时顺带杀掉 daemon。
func StartBackground(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}

// StopRunning 停止当前 PID 文件指向的 daemon 进程。
// Unix 平台使用 SIGTERM，让 daemon 走正常清理流程并删除 PID/socket 等运行态文件。
func StopRunning() error {
	pid, err := ReadPID()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
