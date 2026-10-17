package main

import (
	"embed"
	"os"
	"time"

	"github.com/SonKoz-Game/SonKozGlide/internal/instance"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	winopts "github.com/wailsapp/wails/v2/pkg/options/windows"
	"golang.org/x/sys/windows"
)

//go:embed rules/rules.yaml
var rulesYAML []byte

//go:embed all:frontend/dist
var assets embed.FS

const (
	windowTitle       = "SonKoz Glide"
	instanceMutexName = "SonKozGlide_SingleInstance_Mutex"
	showEventName     = `Local\SonKozGlide_ShowWindow`
	uiFlag            = "-ui"
	hideFlag          = "-hide"
	startupFlag       = "-startup"
	restartFlag       = "-restart"
	reconnectFlag     = "-connect"
	restartWait       = 30 * time.Second
)

func main() {
	if bindingsBuild || hasArg(uiFlag) {
		runUI()
		return
	}
	runCore()
}

func runCore() {
	background := hasArg(hideFlag) || hasArg(startupFlag)

	wait := time.Duration(0)
	if hasArg(restartFlag) {
		wait = restartWait
	}
	handle, ok := instance.Acquire(instanceMutexName, wait)
	if !ok {
		if !background {
			signalRunningInstance()
		}
		return
	}
	defer windows.CloseHandle(handle)
	if background {
		_ = windows.SetPriorityClass(windows.CurrentProcess(), windows.NORMAL_PRIORITY_CLASS)
	}

	showRequests, showErr := createShowEvent()

	core := NewCore(embeddedFiles, rulesYAML)
	core.reconnect = hasArg(reconnectFlag)
	if !background {
		core.showUI()
	}
	core.init()
	if background {
		core.trimWhenIdle()
	}
	if showErr == nil {
		go core.watchShowRequests(showRequests)
	}
	core.run()
}

func runUI() {
	var core *ipcConn
	if !bindingsBuild {
		conn, err := attachCore()
		if err != nil {
			return
		}
		core = conn
	}

	app := NewApp(core)
	if core != nil {
		go app.serveCore()
	}

	err := wails.Run(&options.App{
		Title:         windowTitle,
		Width:         380,
		Height:        600,
		DisableResize: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		Windows: &winopts.Options{
			WebviewGpuIsDisabled: true,
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
