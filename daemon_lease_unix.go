//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// daemonLease 在 Unix 上持有当前用户 daemon 的进程级独占锁。
// 锁文件本身不能删除；进程退出或文件关闭后，内核会自动释放锁。
type daemonLease struct {
	file *os.File
}

func acquireDaemonLease(path string) (*daemonLease, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
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
