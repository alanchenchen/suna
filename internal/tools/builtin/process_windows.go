//go:build windows

package builtin

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree 表示一次命令对应的平台进程树。
type processTree interface {
	terminate(grace time.Duration)
	close()
}

type windowsProcessTree struct {
	job windows.Handle
}

const windowsStartFailureWaitLimit = 2 * time.Second

type assignProcessToJobFunc func(windows.Handle, windows.Handle) error

func startProcessTree(cmd *exec.Cmd) (processTree, error) {
	return startProcessTreeWithAssign(cmd, windows.AssignProcessToJobObject)
}

func startProcessTreeWithAssign(cmd *exec.Cmd, assign assignProcessToJobFunc) (processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job object: %w", err)
	}

	// 复制而不是覆盖调用方的配置，只追加挂起启动标志。
	// 根进程在进入 Job Object 前不会执行，因此不存在创建后代并逃逸的窗口。
	sys := &syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		copy := *cmd.SysProcAttr
		sys = &copy
	}
	sys.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.SysProcAttr = sys
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}

	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		abortStartedProcess(cmd, job)
		return nil, fmt.Errorf("open suspended process: %w", err)
	}
	err = assign(job, process)
	_ = windows.CloseHandle(process)
	if err != nil {
		abortStartedProcess(cmd, job)
		return nil, fmt.Errorf("assign process to job object: %w", err)
	}

	thread, err := openSuspendedMainThread(uint32(cmd.Process.Pid))
	if err != nil {
		abortStartedProcess(cmd, job)
		return nil, fmt.Errorf("find suspended main thread: %w", err)
	}
	previousSuspendCount, err := windows.ResumeThread(thread)
	_ = windows.CloseHandle(thread)
	if err != nil {
		abortStartedProcess(cmd, job)
		return nil, fmt.Errorf("resume suspended main thread: %w", err)
	}
	if previousSuspendCount != 1 {
		abortStartedProcess(cmd, job)
		return nil, fmt.Errorf("resume suspended main thread: unexpected suspend count %d", previousSuspendCount)
	}
	return &windowsProcessTree{job: job}, nil
}

func openSuspendedMainThread(pid uint32) (windows.Handle, error) {
	// CREATE_SUSPENDED 返回时进程只有初始线程；按所属进程查到的线程即主线程。
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return 0, err
	}
	for {
		if entry.OwnerProcessID == pid {
			return windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			return 0, fmt.Errorf("main thread for process %d not found: %w", pid, err)
		}
	}
}

func abortStartedProcess(cmd *exec.Cmd, job windows.Handle) {
	// Start 成功后的任何初始化失败都必须 fail closed。TerminateJob 覆盖已经
	// Assign 的位置，Kill 覆盖 Assign 之前/失败的位置；关闭 Job 也会触发
	// KILL_ON_JOB_CLOSE。Wait 只能有界执行，异常系统状态不能卡住启动调用方。
	_ = windows.TerminateJobObject(job, 1)
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = windows.CloseHandle(job)
	waitStartedProcess(cmd, windowsStartFailureWaitLimit)
}

func waitStartedProcess(cmd *exec.Cmd, limit time.Duration) {
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func (p *windowsProcessTree) terminate(_ time.Duration) {
	if p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
	}
}

func (p *windowsProcessTree) close() {
	if p.job != 0 {
		// KILL_ON_JOB_CLOSE 在正常关闭时清理仍存活的后代，并释放内核句柄。
		_ = windows.CloseHandle(p.job)
		p.job = 0
	}
}
