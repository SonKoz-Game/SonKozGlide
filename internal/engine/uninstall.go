package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	legacyTargetDirName = "SonKozNetwork"
	driverServiceName   = "WinDivert"
	driverModuleName    = "WinDivert.dll"
	driverStopFirstWait = 2 * time.Second
	driverStopWait      = 5 * time.Second
)

var fileRemoveWait = 5 * time.Second

type DriverRemoval string

const (
	DriverAbsent  DriverRemoval = "absent"
	DriverForeign DriverRemoval = "foreign"
	DriverRemoved DriverRemoval = "removed"
	DriverInUse   DriverRemoval = "inuse"
)

type FileRemoval struct {
	Files     int
	Bytes     int64
	Pending   int
	Remaining []string
}

func installDirs() []string {
	target := getTargetDir()
	return []string{target, filepath.Join(filepath.Dir(target), legacyTargetDirName)}
}

func SystemChangesApplied() (dns bool, tuning bool) {
	if backup, err := loadDNSBackup(); err == nil {
		dns = backup.Applied
	}
	if backup, err := loadTuningBackup(); err == nil {
		tuning = backup.Applied
	}
	return dns, tuning
}

func StopInstallProcesses() []string {
	dirs := installDirs()
	return terminateProcesses(func(modules []string) bool {
		for _, module := range modules {
			if pathInDirs(module, dirs) {
				return true
			}
		}
		return false
	})
}

func terminateDriverUsers() []string {
	return terminateProcesses(func(modules []string) bool {
		for _, module := range modules {
			if strings.EqualFold(filepath.Base(module), driverModuleName) {
				return true
			}
		}
		return false
	})
}

func terminateProcesses(match func(modules []string) bool) []string {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	self := uint32(os.Getpid())
	seen := make(map[string]bool)
	var names []string
	var handles []windows.Handle
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		pid := entry.ProcessID
		if pid == self || pid <= 4 || !match(processModules(pid)) {
			continue
		}

		handle, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
		if openErr != nil {
			continue
		}
		if windows.TerminateProcess(handle, 1) != nil {
			_ = windows.CloseHandle(handle)
			continue
		}
		handles = append(handles, handle)

		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	deadline := time.Now().Add(processExitWait)
	for _, handle := range handles {
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 0
		}
		_, _ = windows.WaitForSingleObject(handle, uint32(remaining.Milliseconds()))
		_ = windows.CloseHandle(handle)
	}
	return names
}

func processModules(pid uint32) []string {
	var snapshot windows.Handle
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err = windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
		if !errors.Is(err, windows.ERROR_BAD_LENGTH) {
			break
		}
	}
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	var paths []string
	var entry windows.ModuleEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Module32First(snapshot, &entry); err == nil; err = windows.Module32Next(snapshot, &entry) {
		paths = append(paths, windows.UTF16ToString(entry.ExePath[:]))
	}
	return paths
}

func RemoveDriverService() (DriverRemoval, []string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return DriverAbsent, nil, err
	}
	defer m.Disconnect()

	s, err := m.OpenService(driverServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return DriverAbsent, nil, nil
	}
	if err != nil {
		return DriverAbsent, nil, err
	}
	defer s.Close()

	cfg, err := s.Config()
	if err != nil {
		return DriverAbsent, nil, err
	}
	if !driverImageInDirs(cfg.BinaryPathName, installDirs()) {
		return DriverForeign, nil, nil
	}

	var closed []string
	stopped := stopService(s, driverStopFirstWait)
	if !stopped {
		closed = terminateDriverUsers()
		stopped = stopService(s, driverStopWait)
	}
	if err := s.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return DriverInUse, closed, err
	}
	if !stopped {
		return DriverInUse, closed, nil
	}
	return DriverRemoved, closed, nil
}

func stopService(s *mgr.Service, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	requested := false
	for {
		status, err := s.Query()
		if err == nil && status.State == svc.Stopped {
			return true
		}
		if err == nil && !requested && status.State != svc.StopPending {
			_, _ = s.Control(svc.Stop)
			requested = true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func driverImageInDirs(imagePath string, dirs []string) bool {
	image := strings.Trim(strings.TrimSpace(imagePath), `"`)
	for _, prefix := range []string{`\??\`, `\\?\`} {
		image = strings.TrimPrefix(image, prefix)
	}
	return pathInDirs(image, dirs)
}

func pathInDirs(path string, dirs []string) bool {
	path = strings.ToLower(filepath.Clean(path))
	for _, dir := range dirs {
		if strings.HasPrefix(path, strings.ToLower(filepath.Clean(dir))+`\`) {
			return true
		}
	}
	return false
}

func RemoveFiles() FileRemoval {
	var result FileRemoval
	for _, dir := range installDirs() {
		removeTree(dir, &result)
	}
	return result
}

func removeTree(dir string, result *FileRemoval) {
	before := treeFiles(dir)
	deadline := time.Now().Add(fileRemoveWait)
	for os.RemoveAll(dir) != nil && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
	}

	left := treeFiles(dir)
	for path, size := range before {
		if _, kept := left[path]; !kept {
			result.Files++
			result.Bytes += size
		}
	}
	if _, err := os.Stat(dir); err == nil {
		result.Pending += len(left)
		result.Remaining = append(result.Remaining, dir)
	}
}

func treeFiles(dir string) map[string]int64 {
	files := make(map[string]int64)
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			files[path] = info.Size()
		}
		return nil
	})
	return files
}

var webviewDataPatterns = []string{"SonKozGlide*.exe", "SonKozNetwork*.exe"}

func WebviewDataDirs(exePath string) (others []string, current string) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil, ""
	}

	if exePath != "" {
		if dir := filepath.Join(appData, filepath.Base(exePath)); isWebviewDataDir(dir) {
			current = dir
		}
	}
	for _, pattern := range webviewDataPatterns {
		matches, _ := filepath.Glob(filepath.Join(appData, pattern))
		for _, dir := range matches {
			if !strings.EqualFold(dir, current) && isWebviewDataDir(dir) {
				others = append(others, dir)
			}
		}
	}
	sort.Strings(others)
	return others, current
}

func isWebviewDataDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "EBWebView"))
	return err == nil && info.IsDir()
}

func ScheduleRemovalAfterExit(paths []string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", removalScript(os.Getpid(), paths))
	cmd.Dir = os.TempDir()
	return hideWindow(cmd).Start()
}

func removalScript(pid int, paths []string) string {
	quoted := make([]string, len(paths))
	for i, path := range paths {
		quoted[i] = "'" + strings.ReplaceAll(path, "'", "''") + "'"
	}
	return fmt.Sprintf("$ErrorActionPreference='SilentlyContinue';"+
		"Wait-Process -Id %d -Timeout 60;"+
		"$targets=@(%s);"+
		"for($i=0;$i -lt 40;$i++){"+
		"$left=@($targets|Where-Object{Test-Path -LiteralPath $_});"+
		"if($left.Count -eq 0){break};"+
		"$left|ForEach-Object{Remove-Item -LiteralPath $_ -Recurse -Force};"+
		"Start-Sleep -Milliseconds 500}",
		pid, strings.Join(quoted, ","))
}
