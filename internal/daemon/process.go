package daemon

import (
	"os"
	"strconv"
)

// writePID 只服务后台 local daemon 的进程管理命令；headless runtime 不写 PID 文件。
func (d *Daemon) writePID() error {
	return os.WriteFile(d.cfg.PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0644)
}

// removePID 与 writePID 成对使用，daemon 正常退出时清理后台进程标记。
func (d *Daemon) removePID() {
	os.Remove(d.cfg.PIDPath())
}
