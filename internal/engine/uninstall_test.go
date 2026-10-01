package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriverImageMatchesOnlyOwnDirectories(t *testing.T) {
	dirs := []string{`C:\ProgramData\SonKozGlide`, `C:\ProgramData\SonKozNetwork`}
	cases := map[string]bool{
		`\??\C:\ProgramData\SonKozGlide\WinDivert64.sys`:         true,
		`\??\c:\programdata\sonkoznetwork\WinDivert64.sys`:       true,
		`"C:\ProgramData\SonKozGlide\WinDivert64.sys"`:           true,
		`\??\C:\Program Files\GoodbyeDPI\x86_64\WinDivert64.sys`: false,
		`\??\C:\ProgramData\SonKozGlideBackup\WinDivert64.sys`:   false,
		`\??\C:\ProgramData\SonKozGlide`:                         false,
		`\SystemRoot\System32\drivers\WinDivert64.sys`:           false,
	}

	for image, want := range cases {
		if got := driverImageInDirs(image, dirs); got != want {
			t.Errorf("driverImageInDirs(%q) = %v, want %v", image, got, want)
		}
	}
}

func TestInstallDirsCoverCurrentAndLegacyNames(t *testing.T) {
	useTempTargetDir(t)

	dirs := installDirs()
	if len(dirs) != 2 || filepath.Base(dirs[0]) != TargetDirName || filepath.Base(dirs[1]) != legacyTargetDirName {
		t.Fatalf("unexpected install dirs %v", dirs)
	}
	if filepath.Dir(dirs[0]) != os.Getenv("PROGRAMDATA") || filepath.Dir(dirs[1]) != os.Getenv("PROGRAMDATA") {
		t.Fatalf("install dirs must live directly under ProgramData, got %v", dirs)
	}
}

func writeTestFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0644); err != nil {
		t.Fatal(err)
	}
}

func checkRemoval(t *testing.T, got FileRemoval, files int, bytes int64, pending int, remaining []string) {
	t.Helper()
	if got.Files != files || got.Bytes != bytes || got.Pending != pending || len(got.Remaining) != len(remaining) {
		t.Fatalf("got %+v, want files=%d bytes=%d pending=%d remaining=%v", got, files, bytes, pending, remaining)
	}
	for i := range remaining {
		if got.Remaining[i] != remaining[i] {
			t.Fatalf("got remaining %v, want %v", got.Remaining, remaining)
		}
	}
}

func shortFileRemoveWait(t *testing.T, wait time.Duration) {
	t.Helper()
	old := fileRemoveWait
	fileRemoveWait = wait
	t.Cleanup(func() { fileRemoveWait = old })
}

func TestRemoveTreeDeletesEverything(t *testing.T) {
	dir := filepath.Join(t.TempDir(), TargetDirName)
	writeTestFile(t, filepath.Join(dir, "winws.exe"), 3)
	writeTestFile(t, filepath.Join(dir, "logs", "sk_service.log"), 5)

	var result FileRemoval
	removeTree(dir, &result)
	checkRemoval(t, result, 2, 8, 0, nil)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("install directory still exists")
	}

	removeTree(dir, &result)
	checkRemoval(t, result, 2, 8, 0, nil)
}

func TestRemoveTreeWaitsForAFileToBeReleased(t *testing.T) {
	shortFileRemoveWait(t, 3*time.Second)
	dir := filepath.Join(t.TempDir(), TargetDirName)
	locked := filepath.Join(dir, "sk_service.log")
	writeTestFile(t, locked, 7)
	writeTestFile(t, filepath.Join(dir, "cygwin1.dll"), 4)

	handle, err := os.Open(locked)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		handle.Close()
	}()

	var result FileRemoval
	removeTree(dir, &result)
	checkRemoval(t, result, 2, 11, 0, nil)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a briefly locked file must not leave the directory behind")
	}
}

func TestRemoveTreeReportsFilesStillInUse(t *testing.T) {
	shortFileRemoveWait(t, 300*time.Millisecond)
	dir := filepath.Join(t.TempDir(), TargetDirName)
	locked := filepath.Join(dir, "drivers", "WinDivert64.sys")
	writeTestFile(t, locked, 7)
	writeTestFile(t, filepath.Join(dir, "cygwin1.dll"), 4)

	handle, err := os.Open(locked)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { handle.Close() })

	var result FileRemoval
	removeTree(dir, &result)
	checkRemoval(t, result, 1, 4, 1, []string{dir})
}

func TestStopInstallProcessesTargetsOnlyOurDirectories(t *testing.T) {
	useTempTargetDir(t)
	system := os.Getenv("SystemRoot")
	source, err := os.ReadFile(filepath.Join(system, "System32", "PING.EXE"))
	if err != nil {
		t.Skipf("ping.exe unavailable: %v", err)
	}

	start := func(path string) chan struct{} {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, source, 0755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(path, "-n", "60", "127.0.0.1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		if !waitForProcessStable(cmd, 200*time.Millisecond) {
			t.Fatal("the test process should still be running")
		}
		return done
	}

	insideDone := start(filepath.Join(installDirs()[1], "glide_uninstall_test.exe"))
	outsideDone := start(filepath.Join(t.TempDir(), "glide_uninstall_test.exe"))

	names := StopInstallProcesses()
	if len(names) != 1 || names[0] != "glide_uninstall_test.exe" {
		t.Fatalf("expected exactly the process running from the install dir, got %v", names)
	}
	select {
	case <-insideDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the process running from the install dir is still alive")
	}
	select {
	case <-outsideDone:
		t.Fatal("a same-named process outside the install dirs must not be touched")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWebviewDataDirsFindsOwnProfilesOnly(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)

	for _, name := range []string{"SonKozGlide_windows_amd64.exe", "SonKozGlide.exe", "SonKozNetwork.exe", "glide.exe", "OtherApp.exe"} {
		if err := os.MkdirAll(filepath.Join(appData, name, "EBWebView"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(appData, "SonKozGlide-notes.exe"), 0755); err != nil {
		t.Fatal(err)
	}

	others, current := WebviewDataDirs(`D:\Downloads\glide.exe`)
	if current != filepath.Join(appData, "glide.exe") {
		t.Fatalf("the running executable's profile must be found by its own name, got %q", current)
	}
	want := map[string]bool{
		filepath.Join(appData, "SonKozGlide_windows_amd64.exe"): true,
		filepath.Join(appData, "SonKozGlide.exe"):               true,
		filepath.Join(appData, "SonKozNetwork.exe"):             true,
	}
	if len(others) != len(want) {
		t.Fatalf("got %v, want %v", others, want)
	}
	for _, dir := range others {
		if !want[dir] {
			t.Fatalf("unexpected profile %q in %v", dir, others)
		}
	}

	others, current = WebviewDataDirs(`D:\Downloads\SonKozGlide.exe`)
	if current != filepath.Join(appData, "SonKozGlide.exe") || len(others) != 2 {
		t.Fatalf("the current profile must not be listed twice: current=%q others=%v", current, others)
	}
}

func TestRemovalScriptQuotesPaths(t *testing.T) {
	script := removalScript(4242, []string{`C:\Users\Ali'nin\İndirilenler\SonKozGlide.exe`, `C:\x y\$env.exe`})

	for _, want := range []string{
		"Wait-Process -Id 4242",
		`'C:\Users\Ali''nin\İndirilenler\SonKozGlide.exe'`,
		`'C:\x y\$env.exe'`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script %q does not contain %q", script, want)
		}
	}
	if strings.Contains(script, `"`) {
		t.Fatalf("double quotes would break the command line: %q", script)
	}
}
