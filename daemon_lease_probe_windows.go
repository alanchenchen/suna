//go:build windows

package main

// Windows 不新增文件锁探测；Named Pipe 挂载继续承担重复实例排斥。
func daemonLeaseHeld(string) (bool, error) { return false, nil }
