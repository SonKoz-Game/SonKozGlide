package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/SonKoz-Game/SonKozGlide/internal/engine"
	"github.com/SonKoz-Game/SonKozGlide/internal/settings"
	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	uninstallStopTimeout = 30 * time.Second
	uninstallQuitDelay   = 20 * time.Second
)

type UninstallEvent struct {
	Step    string   `json:"step"`
	State   string   `json:"state"`
	Reason  string   `json:"reason,omitempty"`
	Items   []string `json:"items,omitempty"`
	Files   int      `json:"files,omitempty"`
	Bytes   int64    `json:"bytes,omitempty"`
	Pending int      `json:"pending,omitempty"`
}

func (a *App) Uninstall() []UninstallEvent {
	a.mu.Lock()
	if a.uninstalling {
		a.mu.Unlock()
		return nil
	}
	a.uninstalling = true
	a.mu.Unlock()

	exePath, exeErr := currentExecutablePath()
	dnsApplied, tuningApplied := engine.SystemChangesApplied()
	otherProfiles, currentProfile := engine.WebviewDataDirs(exePath)
	var leftovers []string

	events := []UninstallEvent{
		a.uninstallStep("connection", func() UninstallEvent {
			active := engine.IsProcessRunning()
			if !engine.Shutdown(uninstallStopTimeout) {
				engine.Stop()
			}
			stopped := engine.StopInstallProcesses()
			if !active && len(stopped) == 0 {
				return UninstallEvent{State: "skipped"}
			}
			return UninstallEvent{State: "done", Items: stopped}
		}),

		a.uninstallStep("network", func() UninstallEvent {
			engine.RestoreDNS()
			engine.RestoreNetworkTuning()

			var items []string
			if dnsApplied {
				items = append(items, "dns")
			}
			if tuningApplied {
				items = append(items, "tuning")
			}
			if len(items) == 0 {
				return UninstallEvent{State: "skipped"}
			}
			return UninstallEvent{State: "done", Items: items}
		}),

		a.uninstallStep("autostart", func() UninstallEvent {
			existed := autoStartTaskExists()
			taskErr := deleteAutoStartTask()
			runRemoved := deleteLegacyRunEntry() == nil
			switch {
			case taskErr != nil:
				return UninstallEvent{State: "warn", Reason: "error"}
			case !existed && !runRemoved:
				return UninstallEvent{State: "skipped"}
			}
			return UninstallEvent{State: "done"}
		}),

		a.uninstallStep("driver", func() UninstallEvent {
			result, closed, err := engine.RemoveDriverService()
			switch {
			case result == engine.DriverRemoved:
				return UninstallEvent{State: "done", Items: closed}
			case result == engine.DriverInUse:
				return UninstallEvent{State: "warn", Reason: string(result), Items: closed}
			case err != nil:
				return UninstallEvent{State: "warn", Reason: "error"}
			}
			return UninstallEvent{State: "skipped", Reason: string(result)}
		}),

		a.uninstallStep("files", func() UninstallEvent {
			result := engine.RemoveFiles()
			leftovers = result.Remaining
			if result.Files == 0 && result.Pending == 0 {
				return UninstallEvent{State: "skipped"}
			}
			return UninstallEvent{State: "done", Files: result.Files, Bytes: result.Bytes, Pending: result.Pending}
		}),

		a.uninstallStep("data", func() UninstallEvent {
			var items []string
			removed, err := settings.Remove()
			if removed {
				items = append(items, "settings")
			}
			for _, dir := range otherProfiles {
				if os.RemoveAll(dir) != nil {
					err = os.ErrPermission
				}
			}
			if len(otherProfiles) > 0 || currentProfile != "" {
				items = append(items, "webview")
			}

			switch {
			case err != nil:
				return UninstallEvent{State: "warn", Items: items, Reason: "error"}
			case len(items) == 0:
				return UninstallEvent{State: "skipped"}
			}
			return UninstallEvent{State: "done", Items: items}
		}),

		a.uninstallStep("app", func() UninstallEvent {
			targets := append([]string{}, leftovers...)
			if currentProfile != "" {
				targets = append(targets, currentProfile)
			}
			if exeErr == nil {
				dir, name := filepath.Split(exePath)
				targets = append(targets,
					exePath,
					filepath.Join(dir, "."+name+".old"),
					filepath.Join(dir, "."+name+".new"),
				)
			}
			if err := engine.ScheduleRemovalAfterExit(targets); err != nil || exeErr != nil {
				return UninstallEvent{State: "failed"}
			}
			return UninstallEvent{State: "done"}
		}),
	}

	time.AfterFunc(uninstallQuitDelay, a.quit)
	return events
}

func (a *App) FinishUninstall() {
	a.mu.Lock()
	uninstalling := a.uninstalling
	a.mu.Unlock()

	if uninstalling {
		a.quit()
	}
}

func (a *App) uninstallStep(step string, run func() UninstallEvent) UninstallEvent {
	a.emitUninstall(UninstallEvent{Step: step, State: "active"})
	event := run()
	event.Step = step
	a.emitUninstall(event)
	return event
}

func (a *App) emitUninstall(event UninstallEvent) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "uninstall_progress", event)
	}
}

func (a *App) quit() {
	a.quitOnce.Do(func() {
		systray.Quit()
		runtime.Quit(a.ctx)
	})
}
