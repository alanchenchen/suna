//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Unix lease 可被启动协调器探测；child 退出且 lease 已释放时，可以立即确认没有并发 winner。
const daemonLeaseProbeSupported = true

// daemonLease 在 Unix 上持有当前用户 daemon 的进程级独占锁。
// 锁文件本身不能删除；进程退出或文件关闭后，内核会自动释放锁。
type daemonLease struct {
	file *os.File
}

func acquireDaemonLease(path string) (*daemonLease, error) {
	file, err := openDaemonLeaseFile(path)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, fmt.Errorf("daemon already running")
		}
		return nil, fmt.Errorf("lock daemon: %w", err)
	}
	return &daemonLease{file: file}, nil
}

func (l *daemonLease) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return fmt.Errorf("unlock daemon: %w", unlockErr)
	}
	return closeErr
}

// daemonLeaseHeld 只探测当前 lock inode 是否由其他进程持有，不改变文件内容和生命周期。
func daemonLeaseHeld(path string) (bool, error) {
	file, err := openDaemonLeaseFile(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return true, nil
		}
		return false, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		return false, err
	}
	return false, nil
}

func openDaemonLeaseFile(path string) (*os.File, error) {
	// 启动协调会先探测 lock；首次启动时必须在探测前创建数据目录。
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
}

func startBackground(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}

func fallbackStopProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
