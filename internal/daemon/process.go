package daemon

import (
	"os"
	"strconv"
	"strings"
)

// writePID 在 daemon 已取得单实例所有权并挂载全部 transport 后发布当前 PID。
func (d *Daemon) writePID() error {
	return os.WriteFile(d.cfg.PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0644)
}

// removePID 仅删除仍属于当前进程的 PID 文件，避免旧实例退出时清理新实例的发现状态。
func (d *Daemon) removePID() {
	path := d.cfg.PIDPath()
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		return
	}
	_ = os.Remove(path)
}
