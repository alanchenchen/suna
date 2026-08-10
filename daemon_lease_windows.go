//go:build windows

package main

// daemonLease 在 Windows 上不增加额外文件锁；Named Pipe 的首实例语义负责排斥重复 daemon。
type daemonLease struct{}

func acquireDaemonLease(string) (*daemonLease, error) {
	return &daemonLease{}, nil
}

func (*daemonLease) Close() error { return nil }
