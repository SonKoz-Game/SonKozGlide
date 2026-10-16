package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/SonKoz-Game/SonKozGlide/internal/engine"
	"github.com/SonKoz-Game/SonKozGlide/internal/settings"
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

func (c *Core) Uninstall() []UninstallEvent {
	c.mu.Lock()
	if c.uninstalling {
		c.mu.Unlock()
		return nil
	}
	c.uninstalling = true
	c.mu.Unlock()

	exePath, exeErr := currentExecutablePath()
	dnsApplied, tuningApplied := engine.SystemChangesApplied()
	otherProfiles, currentProfile := engine.WebviewDataDirs(exePath)
	var leftovers []string

	events := []UninstallEvent{
		c.uninstallStep("connection", func() UninstallEvent {
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

		c.uninstallStep("network", func() UninstallEvent {
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

		c.uninstallStep("autostart", func() UninstallEvent {
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

		c.uninstallStep("driver", func() UninstallEvent {
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

		c.uninstallStep("files", func() UninstallEvent {
			result := engine.RemoveFiles()
			leftovers = result.Remaining
			if result.Files == 0 && result.Pending == 0 {
				return UninstallEvent{State: "skipped"}
			}
			return UninstallEvent{State: "done", Files: result.Files, Bytes: result.Bytes, Pending: result.Pending}
		}),

		c.uninstallStep("data", func() UninstallEvent {
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

		c.uninstallStep("app", func() UninstallEvent {
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
			if err := engine.ScheduleRemovalAfterExit(targets, c.uiPID()); err != nil || exeErr != nil {
				return UninstallEvent{State: "failed"}
			}
			return UninstallEvent{State: "done"}
		}),
	}

	time.AfterFunc(uninstallQuitDelay, c.quit)
	return events
}

func (c *Core) FinishUninstall() {
	c.mu.Lock()
	uninstalling := c.uninstalling
	c.mu.Unlock()

	if uninstalling {
		c.quit()
	}
}

func (c *Core) uninstallStep(step string, run func() UninstallEvent) UninstallEvent {
	c.emitUninstall(UninstallEvent{Step: step, State: "active"})
	event := run()
	event.Step = step
	c.emitUninstall(event)
	return event
}

func (c *Core) emitUninstall(event UninstallEvent) {
	c.emit("uninstall_progress", event)
}
