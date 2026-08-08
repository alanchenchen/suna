//go:build !windows

package builtin

import (
	"os/exec"
	"syscall"
	"time"
)

// processTree 表示一次命令对应的平台进程树。
type processTree interface {
	terminate(grace time.Duration)
	close()
}

type unixProcessTree struct{ pgid int }

func startProcessTree(cmd *exec.Cmd) (processTree, error) {
	// 独立进程组确保 shell 创建的所有后代都能按组回收。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &unixProcessTree{pgid: cmd.Process.Pid}, nil
}

func (p *unixProcessTree) terminate(grace time.Duration) {
	if p.pgid <= 0 {
		return
	}
	_ = syscall.Kill(-p.pgid, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-p.pgid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
}

func (*unixProcessTree) close() {}
