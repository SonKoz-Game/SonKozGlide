package main

import (
	"embed"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/SonKoz-Game/SonKozGlide/internal/instance"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"golang.org/x/sys/windows"
)

//go:embed resources/*
var embeddedFiles embed.FS

//go:embed rules/rules.yaml
var rulesYAML []byte

//go:embed all:frontend/dist
var assets embed.FS

const (
	windowTitle       = "SonKoz Glide"
	instanceMutexName = "SonKozGlide_SingleInstance_Mutex"
	restartFlag       = "-restart"
	reconnectFlag     = "-connect"
	restartWait       = 30 * time.Second
)

func main() {
	wait := time.Duration(0)
	if hasArg(restartFlag) {
		wait = restartWait
	}
	handle, ok := instance.Acquire(instanceMutexName, wait)
	if !ok {
		focusExistingWindow()
		os.Exit(0)
	}
	defer windows.CloseHandle(handle)

	app := NewApp(embeddedFiles, rulesYAML)
	app.reconnect = hasArg(reconnectFlag)

	err := wails.Run(&options.App{
		Title:             windowTitle,
		Width:             380,
		Height:            600,
		DisableResize:     true,
		HideWindowOnClose: true,
		StartHidden:       hasArg("-hide") || hasArg("-startup"),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

func hasArg(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == name {
			return true
		}
	}
	return false
}

func focusExistingWindow() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	showWindow := user32.NewProc("ShowWindow")
	setForeground := user32.NewProc("SetForegroundWindow")

	title, err := syscall.UTF16PtrFromString(windowTitle)
	if err != nil {
		return
	}

	hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}

	const swRestore = 9
	showWindow.Call(hwnd, swRestore)
	setForeground.Call(hwnd)
}
