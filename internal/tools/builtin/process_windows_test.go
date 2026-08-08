//go:build windows

package builtin

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	windowsBehaviorTestLimit = 8 * time.Second
	windowsHelperRoleEnv     = "SUNA_WINDOWS_PROCESS_TEST_HELPER"
	windowsHelperMarkerEnv   = "SUNA_WINDOWS_PROCESS_TEST_MARKER"
	windowsHelperRoot        = "root"
	windowsHelperChild       = "child"
	windowsHelperRootExit    = "exit"
	windowsHelperRootWait    = "wait"
)

func TestWindowsTimeoutAndTerminateCleanPipeDescendant(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "descendant.pid")
		started := time.Now()
		result := runForeground(t.Context(), windowsHelperTreeCommand(t, marker, windowsHelperRootWait), 2*time.Second)
		if !result.IsError || result.Metadata["exec_status"] != execStatusTimedOut {
			t.Fatalf("timeout result = %#v", result)
		}
		if elapsed := time.Since(started); elapsed > 2*time.Second+execWaitLimit+execOutputDrainLimit+time.Second {
			t.Fatalf("timeout cleanup took %s", elapsed)
		}
		pid := waitPIDFile(t, marker, time.Second)
		waitProcessGone(t, pid, 3*time.Second)
	})

	t.Run("terminate", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "descendant.pid")
		cmd := windowsHelperTreeCommand(t, marker, windowsHelperRootWait)
		run, err := startManagedProcess(cmd, io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("start process tree: %v", err)
		}
		pid := waitPIDFile(t, marker, windowsBehaviorTestLimit)
		started := time.Now()
		run.tree.terminate(execTerminateGrace)
		if err := waitProcess(run.wait, execWaitLimit); err != nil {
			t.Fatalf("root did not exit after termination: %v", err)
		}
		run.finishOutput(execOutputDrainLimit)
		run.tree.close()
		if elapsed := time.Since(started); elapsed > execWaitLimit+execOutputDrainLimit+time.Second {
			t.Fatalf("termination cleanup took %s", elapsed)
		}
		waitProcessGone(t, pid, 3*time.Second)
	})
}

func TestWindowsNormalRootExitCleansPipeDescendant(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant.pid")
	cmd := windowsHelperTreeCommand(t, marker, windowsHelperRootExit)
	run, err := startManagedProcess(cmd, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("start process tree: %v", err)
	}
	pid := waitPIDFile(t, marker, windowsBehaviorTestLimit)
	select {
	case err := <-run.wait:
		if err != nil {
			t.Fatalf("root exit: %v", err)
		}
	case <-time.After(windowsBehaviorTestLimit):
		t.Fatal("root did not exit")
	}

	// 后代通过 helper 根进程显式继承输出句柄；进程树清理前，输出排空必须仍被阻塞。
	select {
	case <-run.drainDone:
		run.tree.terminate(execTerminateGrace)
		run.tree.close()
		t.Fatal("output pipes closed before the live descendant was cleaned")
	case <-time.After(100 * time.Millisecond):
	}

	started := time.Now()
	run.tree.terminate(execTerminateGrace)
	run.finishOutput(execOutputDrainLimit)
	run.tree.close()
	if elapsed := time.Since(started); elapsed > execOutputDrainLimit+time.Second {
		t.Fatalf("normal-exit descendant cleanup took %s", elapsed)
	}
	waitProcessGone(t, pid, 3*time.Second)
}

func TestWindowsStartFailuresReturnAndFailClosed(t *testing.T) {
	t.Run("executable-not-found", func(t *testing.T) {
		cmd := exec.Command(filepath.Join(t.TempDir(), "missing-command.exe"))
		started := time.Now()
		tree, err := startProcessTree(cmd)
		if err == nil || tree != nil {
			t.Fatalf("startProcessTree = (%v, %v), want nil tree and error", tree, err)
		}
		if elapsed := time.Since(started); elapsed > windowsStartFailureWaitLimit+time.Second {
			t.Fatalf("ordinary start failure took %s", elapsed)
		}
	})

	t.Run("assignment-failure", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "must-not-exist")
		cmd := exec.Command(systemCMD(t), "/d", "/c", "echo executed>\""+marker+"\"")
		original := &syscall.SysProcAttr{
			CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
			HideWindow:    true,
		}
		cmd.SysProcAttr = original
		assignCalled := false
		started := time.Now()
		tree, err := startProcessTreeWithAssign(cmd, func(windows.Handle, windows.Handle) error {
			assignCalled = true
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("suspended process executed before assignment: stat error %v", statErr)
			}
			return windows.ERROR_ACCESS_DENIED
		})
		if err == nil || tree != nil || !assignCalled {
			t.Fatalf("injected assignment failure = (%v, %v), called=%v", tree, err, assignCalled)
		}
		if elapsed := time.Since(started); elapsed > windowsStartFailureWaitLimit+time.Second {
			t.Fatalf("assignment failure cleanup took %s", elapsed)
		}
		if cmd.SysProcAttr == original {
			t.Fatal("startProcessTree mutated the caller-owned SysProcAttr")
		}
		if original.CreationFlags != windows.CREATE_NEW_PROCESS_GROUP || !original.HideWindow {
			t.Fatalf("caller SysProcAttr changed: %#v", original)
		}
		if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 || !cmd.SysProcAttr.HideWindow {
			t.Fatalf("SysProcAttr fields were not preserved: %#v", cmd.SysProcAttr)
		}
		if cmd.SysProcAttr.CreationFlags&windows.CREATE_SUSPENDED == 0 {
			t.Fatalf("CREATE_SUSPENDED not added: %#v", cmd.SysProcAttr)
		}
		if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed assignment did not fail closed: stat error %v", statErr)
		}
	})
}

// TestWindowsProcessHelper 只由测试子进程显式选择。普通 go test 没有 helper
// 环境角色，因此会直接返回，不会创建任何进程。
func TestWindowsProcessHelper(t *testing.T) {
	role := os.Getenv(windowsHelperRoleEnv)
	if role == "" {
		return
	}

	switch role {
	case windowsHelperRoot:
		if len(os.Args) < 2 {
			t.Fatal("helper root mode is missing")
		}
		mode := os.Args[len(os.Args)-1]
		if mode != windowsHelperRootExit && mode != windowsHelperRootWait {
			t.Fatalf("invalid helper root mode %q", mode)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
		child.Env = windowsHelperEnv(os.Environ(), windowsHelperChild, os.Getenv(windowsHelperMarkerEnv))
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatalf("start helper child: %v", err)
		}
		if mode == windowsHelperRootExit {
			if err := child.Process.Release(); err != nil {
				t.Fatalf("release helper child: %v", err)
			}
			return
		}
		if err := child.Wait(); err != nil {
			t.Fatalf("wait for helper child: %v", err)
		}
	case windowsHelperChild:
		marker := os.Getenv(windowsHelperMarkerEnv)
		if marker == "" {
			t.Fatal("helper child marker is missing")
		}
		if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatalf("write helper child PID: %v", err)
		}
		time.Sleep(time.Hour)
	default:
		t.Fatalf("invalid helper role %q", role)
	}
}

func windowsHelperTreeCommand(t *testing.T, marker, rootMode string) *exec.Cmd {
	t.Helper()
	if rootMode != windowsHelperRootExit && rootMode != windowsHelperRootWait {
		t.Fatalf("invalid helper root mode %q", rootMode)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$", "--", rootMode)
	cmd.Env = windowsHelperEnv(os.Environ(), windowsHelperRoot, marker)
	return cmd
}

func windowsHelperEnv(base []string, role, marker string) []string {
	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, windowsHelperRoleEnv) || strings.EqualFold(name, windowsHelperMarkerEnv) {
			continue
		}
		env = append(env, entry)
	}
	return append(env, windowsHelperRoleEnv+"="+role, windowsHelperMarkerEnv+"="+marker)
}

func systemCMD(t *testing.T) string {
	t.Helper()
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("system cmd.exe unavailable: %v", err)
	}
	return path
}

func waitPIDFile(t *testing.T, path string, limit time.Duration) uint32 {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if parseErr != nil || pid == 0 {
				t.Fatalf("invalid descendant PID %q: %v", data, parseErr)
			}
			return uint32(pid)
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read descendant PID: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant did not create PID marker within %s", limit)
	return 0
}

func waitProcessGone(t *testing.T, pid uint32, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return
		}
		if err != nil {
			t.Fatalf("open descendant %d: %v", pid, err)
		}
		state, waitErr := windows.WaitForSingleObject(handle, 0)
		_ = windows.CloseHandle(handle)
		if waitErr != nil {
			t.Fatalf("query descendant %d: %v", pid, waitErr)
		}
		if state == windows.WAIT_OBJECT_0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant %d survived process-tree cleanup", pid)
}
