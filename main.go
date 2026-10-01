package main

import (
	"embed"
	"os"
	"syscall"
	"unsafe"

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

const windowTitle = "SonKoz Glide"

func main() {
	mutexName := "SonKozGlide_SingleInstance_Mutex"
	uint16MutexName, _ := syscall.UTF16PtrFromString(mutexName)
	handle, err := windows.CreateMutex(nil, true, uint16MutexName)
	if err == windows.ERROR_ALREADY_EXISTS {
		focusExistingWindow()
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		os.Exit(0)
	}
	if handle != 0 {
		defer windows.CloseHandle(handle)
	}

	app := NewApp(embeddedFiles, rulesYAML)

	startHidden := false
	for _, arg := range os.Args {
		if arg == "-hide" || arg == "-startup" {
			startHidden = true
			break
		}
	}

	err = wails.Run(&options.App{
		Title:             windowTitle,
		Width:             380,
		Height:            600,
		DisableResize:     true,
		HideWindowOnClose: true,
		StartHidden:       startHidden,
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
