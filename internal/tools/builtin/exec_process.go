package builtin

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type managedProcess struct {
	tree      processTree
	wait      chan error
	readers   []*os.File
	drainDone chan struct{}
}

func startManagedProcess(cmd *exec.Cmd, stdout, stderr io.Writer) (*managedProcess, error) {
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		_ = outR.Close()
		_ = outW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	tree, err := startProcessTree(cmd)
	if err != nil {
		_ = outR.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		return nil, err
	}
	// 父进程必须立即关闭写端，只保留子进程继承的描述符。
	_ = outW.Close()
	_ = errW.Close()
	run := &managedProcess{tree: tree, wait: make(chan error, 1), readers: []*os.File{outR, errR}, drainDone: make(chan struct{})}
	var drains sync.WaitGroup
	drains.Add(2)
	go func() { defer drains.Done(); _, _ = io.Copy(stdout, outR) }()
	go func() { defer drains.Done(); _, _ = io.Copy(stderr, errR) }()
	go func() { drains.Wait(); close(run.drainDone) }()
	go func() { run.wait <- cmd.Wait() }()
	return run, nil
}

// finishOutput 在上限内完成自然排空时返回 true；超时后会关闭读端强制解阻。
func (p *managedProcess) finishOutput(limit time.Duration) bool {
	if waitSignalWithin(p.drainDone, limit) {
		for _, reader := range p.readers {
			_ = reader.Close()
		}
		return true
	}
	// 硬上限到达后关闭读端，强制解除本地排空协程。
	for _, reader := range p.readers {
		_ = reader.Close()
	}
	_ = waitSignalWithin(p.drainDone, 100*time.Millisecond)
	return false
}

func waitSignalWithin(done <-chan struct{}, limit time.Duration) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func waitProcessWithin(wait <-chan error, limit time.Duration) (error, bool) {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-wait:
		return err, true
	case <-timer.C:
		return fmt.Errorf("process did not exit after termination"), false
	}
}

func waitProcess(wait <-chan error, limit time.Duration) error {
	err, _ := waitProcessWithin(wait, limit)
	return err
}

func remainingExecTimeout(started time.Time, timeout time.Duration) time.Duration {
	remaining := timeout - time.Since(started)
	if remaining <= 0 {
		return time.Nanosecond
	}
	return remaining
}

// execCleanupStatus 只表示 Wait 与输出排空完成度，不表示平台进程树终止结果。
func execCleanupStatus(waitComplete, outputComplete bool) string {
	if waitComplete && outputComplete {
		return "complete"
	}
	return "partial"
}

// foregroundCleanupStatus 保留前台调用语义，与后台任务采用同一判定标准。
func foregroundCleanupStatus(waitComplete, outputComplete bool) string {
	return execCleanupStatus(waitComplete, outputComplete)
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
