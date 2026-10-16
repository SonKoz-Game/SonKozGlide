package main

import (
	"context"
	"sync"

	"github.com/SonKoz-Game/SonKozGlide/internal/engine"
	"github.com/SonKoz-Game/SonKozGlide/internal/settings"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	uiShowEvent       = "window:show"
	uiQuitEvent       = "window:quit"
	uiVisibilityEvent = "window_visibility"
)

type App struct {
	core *ipcConn

	mu       sync.Mutex
	ctx      context.Context
	quitting bool
}

func NewApp(core *ipcConn) *App {
	return &App{core: core}
}

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	quitting := a.quitting
	a.mu.Unlock()

	if quitting {
		runtime.Quit(ctx)
		return
	}
	go watchMinimize(func(minimized bool) {
		runtime.EventsEmit(ctx, uiVisibilityEvent, !minimized)
	})
}

func (a *App) serveCore() {
	a.core.serve(a.handleCore)
	a.quit()
}

func (a *App) handleCore(msg ipcMessage) {
	ctx := a.context()
	switch msg.Event {
	case "":
	case uiQuitEvent:
		a.quit()
	case uiShowEvent:
		if ctx != nil {
			runtime.WindowShow(ctx)
		}
	default:
		if ctx != nil {
			runtime.EventsEmit(ctx, msg.Event, msg.Data)
		}
	}
}

func (a *App) context() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

func (a *App) quit() {
	a.mu.Lock()
	a.quitting = true
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.Quit(ctx)
	}
}

func request[T any](a *App, method string, params ...any) (T, error) {
	var result T
	err := a.core.call(method, &result, params...)
	return result, err
}

func (a *App) StartBypass() (string, error) {
	return request[string](a, "StartBypass")
}

func (a *App) StopBypass() (string, error) {
	return request[string](a, "StopBypass")
}

func (a *App) GetState() (engine.Status, error) {
	return request[engine.Status](a, "GetState")
}

func (a *App) GetTuningReport() (engine.TuningReport, error) {
	return request[engine.TuningReport](a, "GetTuningReport")
}

func (a *App) GetConnectTrace() ([]engine.ProgressEvent, error) {
	return request[[]engine.ProgressEvent](a, "GetConnectTrace")
}

func (a *App) RunServiceCheck() ([]engine.ServiceCheck, error) {
	return request[[]engine.ServiceCheck](a, "RunServiceCheck")
}

func (a *App) GetBootReport() (BootReport, error) {
	return request[BootReport](a, "GetBootReport")
}

func (a *App) GetAutoStart() (bool, error) {
	return request[bool](a, "GetAutoStart")
}

func (a *App) SetAutoStart(enable bool) (string, error) {
	return request[string](a, "SetAutoStart", enable)
}

func (a *App) GetLogs() (string, error) {
	return request[string](a, "GetLogs")
}

func (a *App) GetAppVersion() (string, error) {
	return request[string](a, "GetAppVersion")
}

func (a *App) GetPendingUpdate() (string, error) {
	return request[string](a, "GetPendingUpdate")
}

func (a *App) GetSettings() (settings.Config, error) {
	return request[settings.Config](a, "GetSettings")
}

func (a *App) UpdateSettings(cfg settings.Config) (string, error) {
	return request[string](a, "UpdateSettings", cfg)
}

func (a *App) InstallUpdate() (string, error) {
	return request[string](a, "InstallUpdate")
}

func (a *App) RestartApp() (string, error) {
	return request[string](a, "RestartApp")
}

func (a *App) Uninstall() ([]UninstallEvent, error) {
	return request[[]UninstallEvent](a, "Uninstall")
}

func (a *App) FinishUninstall() error {
	return a.core.call("FinishUninstall", nil)
}
