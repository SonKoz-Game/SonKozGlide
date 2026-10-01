package engine

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

func runHidden(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := hideWindow(exec.CommandContext(ctx, name, args...)).CombinedOutput()
	return string(out), err
}

func runHiddenStatus(timeout time.Duration, name string, args ...string) bool {
	_, err := runHidden(timeout, name, args...)
	return err == nil
}

func runPSOutput(timeout time.Duration, script string) (string, error) {
	return runHidden(timeout, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}
