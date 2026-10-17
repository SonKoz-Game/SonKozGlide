package engine

import (
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	archX64 = "x64"
	archX86 = "x86"
)

var nativeArch = detectNativeArch()

func detectNativeArch() string {
	if runtime.GOARCH != "386" {
		return archX64
	}
	var wow64 bool
	if err := windows.IsWow64Process(windows.CurrentProcess(), &wow64); err == nil && wow64 {
		return archX64
	}
	return archX86
}

func NativeArch() string {
	return nativeArch
}

func driverFileFor(arch string) string {
	if arch == archX86 {
		return "WinDivert32.sys"
	}
	return "WinDivert64.sys"
}

func driverFile() string {
	return driverFileFor(nativeArch)
}
