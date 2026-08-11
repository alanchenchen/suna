//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

// daemonLease 在 Windows 上不增加额外文件锁；Named Pipe 的首实例语义负责排斥重复 daemon。
type daemonLease struct{}

func acquireDaemonLease(string) (*daemonLease, error) {
	return &daemonLease{}, nil
}

func (*daemonLease) Close() error { return nil }

// Windows 不新增文件锁探测；Named Pipe 挂载继续承担重复实例排斥。
const daemonLeaseProbeSupported = false

func daemonLeaseHeld(string) (bool, error) { return false, nil }

func startBackground(cmd *exec.Cmd) error {
	// Windows 上不使用 DETACHED_PROCESS：实测部分终端环境下会导致 CLI 自身输出异常。
	// stdio 已由调用方统一配置；这里只设置进程属性，避免覆盖 app.log 等诊断输出。
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup,
		HideWindow:    true,
	}
	return cmd.Start()
}

func fallbackStopProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
