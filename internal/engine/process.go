package engine

import (
	"os/exec"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	brokerExe       = "winws.exe"
	legacyBrokerExe = "sk_broker.exe"
)

var brokerImageNames = []string{brokerExe, legacyBrokerExe}

const processExitWait = 2 * time.Second

func CleanOldServices() {
	if terminateProcessesByName(brokerImageNames, processExitWait) {
		return
	}
	for _, name := range brokerImageNames {
		runHidden(3*time.Second, "taskkill", "/F", "/IM", name, "/T")
	}
}

func terminateProcessesByName(names []string, wait time.Duration) bool {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)

	ok := true
	var handles []windows.Handle
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if !imageNameMatches(windows.UTF16ToString(entry.ExeFile[:]), names) {
			continue
		}

		handle, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, entry.ProcessID)
		if openErr != nil {
			ok = false
			continue
		}
		_ = windows.TerminateProcess(handle, 1)
		handles = append(handles, handle)
	}

	deadline := time.Now().Add(wait)
	for _, handle := range handles {
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 0
		}
		event, waitErr := windows.WaitForSingleObject(handle, uint32(remaining.Milliseconds()))
		if waitErr != nil || event != windows.WAIT_OBJECT_0 {
			ok = false
		}
		_ = windows.CloseHandle(handle)
	}
	return ok
}

func imageNameMatches(image string, names []string) bool {
	for _, name := range names {
		if strings.EqualFold(image, name) {
			return true
		}
	}
	return false
}

func waitForProcessStable(cmd *exec.Cmd, window time.Duration) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}

	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	event, err := windows.WaitForSingleObject(handle, uint32(window.Milliseconds()))
	if err != nil {
		return isProcessAlive(cmd)
	}
	return event == uint32(windows.WAIT_TIMEOUT)
}

func isProcessAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return false
	}

	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return true
	}

	return status == uint32(windows.WAIT_TIMEOUT)
}
