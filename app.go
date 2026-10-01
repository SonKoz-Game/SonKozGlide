package main

import (
	"context"
	"embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SonKoz-Game/SonKozGlide/internal/engine"
	"github.com/SonKoz-Game/SonKozGlide/internal/settings"
	"github.com/SonKoz-Game/SonKozGlide/internal/updater"
	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed resources/icon_on.ico
var iconOn []byte

//go:embed resources/icon_off.ico
var iconOff []byte

type App struct {
	ctx            context.Context
	updateMenuItem *systray.MenuItem
	resources      embed.FS
	rulesYAML      []byte

	mu            sync.Mutex
	pendingUpdate string
	startupError  string
}

const (
	autoStartTaskName     = "SonKozGlide"
	autoStartRegistryName = "SonKozGlide"
	runRegistryPath       = `Software\Microsoft\Windows\CurrentVersion\Run`
)

func NewApp(resources embed.FS, rulesYAML []byte) *App {
	return &App{resources: resources, rulesYAML: rulesYAML}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	if err := engine.Setup(a.resources); err != nil {
		a.mu.Lock()
		a.startupError = "Gerekli dosyalar cikartilamadi. Uygulamayi yonetici olarak calistirin. (" + err.Error() + ")"
		a.mu.Unlock()
	}
	_ = engine.ApplyRuleset(a.rulesYAML)

	conf := settings.Get()
	engine.SetProfile(engine.ISPProfile(conf.ISPProfile))
	engine.SetSystemOptimizations(conf.SafeDNS, conf.NetworkTuning)

	engine.SetStatusCallback(func(status engine.Status) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "service_state", status)
		}
		a.applyTrayState(status)
	})

	engine.SetErrorCallback(func(message string) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "service_error", message)
		}
	})

	engine.SetProgressCallback(func(event engine.ProgressEvent) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "service_progress", event)
		}
	})

	go func() {
		systray.Run(a.onReady, a.onExit)
	}()
	go a.migrateLegacyAutoStart()
	go a.periodicUpdateCheck()
}

func (a *App) applyTrayState(status engine.Status) {
	switch status.Phase {
	case engine.PhaseRunning:
		systray.SetIcon(iconOn)
		systray.SetTooltip("SonKoz Glide - Baglanti aktif")
	case engine.PhaseStarting, engine.PhaseRecovering:
		systray.SetIcon(iconOn)
		systray.SetTooltip("SonKoz Glide - Baglanti hazirlaniyor")
	default:
		systray.SetIcon(iconOff)
		systray.SetTooltip("SonKoz Glide - Baglanti kapali")
	}
}

func (a *App) periodicUpdateCheck() {
	time.Sleep(5 * time.Second)

	for {
		if settings.Get().AutoUpdate {
			if latest, err := updater.Check(); err == nil && latest != nil {
				a.mu.Lock()
				isNew := a.pendingUpdate != latest.Version()
				a.pendingUpdate = latest.Version()
				a.mu.Unlock()

				if a.updateMenuItem != nil {
					a.updateMenuItem.Show()
				}
				if isNew && a.ctx != nil {
					runtime.EventsEmit(a.ctx, "update_available", latest.Version())
				}
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

func (a *App) GetPendingUpdate() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pendingUpdate
}

func (a *App) onReady() {
	systray.SetIcon(iconOff)
	systray.SetTitle("SonKoz Glide")
	systray.SetTooltip("SonKoz Glide")

	a.updateMenuItem = systray.AddMenuItem("Versiyon Guncellemesi", "Yeni surum mevcut")
	a.updateMenuItem.Hide()
	systray.AddSeparator()

	mShow := systray.AddMenuItem("Arayuzu Ac", "Arayuzu ac")
	mQuit := systray.AddMenuItem("Tamamen Cikis", "Uygulamayi tamamen kapat")

	go func() {
		for {
			select {
			case <-a.updateMenuItem.ClickedCh:
				runtime.WindowShow(a.ctx)
				latest, _ := updater.Check()
				if latest != nil {
					runtime.EventsEmit(a.ctx, "update_available", latest.Version())
				}
			case <-mShow.ClickedCh:
				runtime.WindowShow(a.ctx)
			case <-mQuit.ClickedCh:
				engine.Stop()
				systray.Quit()
				runtime.Quit(a.ctx)
			}
		}
	}()

	conf := settings.Get()
	if conf.AutoStartBypass && !engine.IsProcessRunning() {
		engine.RequestStart()
	} else {
		go func() {
			engine.RestoreDNS()
			engine.RestoreNetworkTuning()
		}()
	}
}

func (a *App) onExit() {
	engine.Stop()
}

func (a *App) StartBypass() string {
	engine.RequestStart()
	return "OK"
}

func (a *App) StopBypass() string {
	engine.RequestStop()
	return "OK"
}

func (a *App) GetState() engine.Status {
	return engine.State()
}

func (a *App) GetTuningReport() engine.TuningReport {
	return engine.GetTuningReport()
}

func (a *App) GetConnectTrace() []engine.ProgressEvent {
	return engine.ConnectTrace()
}

func (a *App) RunServiceCheck() []engine.ServiceCheck {
	return engine.CheckServices()
}

type BootReport struct {
	Info         engine.BootInfo `json:"info"`
	Version      string          `json:"version"`
	Elevated     bool            `json:"elevated"`
	AutoConnect  bool            `json:"autoConnect"`
	StartupError string          `json:"startupError"`
}

func (a *App) GetBootReport() BootReport {
	a.mu.Lock()
	startupError := a.startupError
	a.mu.Unlock()

	return BootReport{
		Info:         engine.GetBootInfo(),
		Version:      updater.GetVersion(),
		Elevated:     windows.GetCurrentProcessToken().IsElevated(),
		AutoConnect:  settings.Get().AutoStartBypass,
		StartupError: startupError,
	}
}

func (a *App) GetAutoStart() bool {
	exePath, err := currentExecutablePath()
	if err != nil {
		return false
	}
	return autoStartTaskMatches(exePath)
}

func (a *App) SetAutoStart(enable bool) string {
	if enable {
		exePath, err := currentExecutablePath()
		if err != nil {
			return "EXE yolu alinamadi: " + err.Error()
		}
		if err := createAutoStartTask(exePath); err != nil {
			return "Baslangic gorevi olusturulamadi: " + err.Error()
		}
		_ = deleteLegacyRunEntry()
	} else {
		if err := deleteAutoStartTask(); err != nil {
			return "Baslangic gorevi kaldirilamadi: " + err.Error()
		}
		_ = deleteLegacyRunEntry()
	}
	return "OK"
}

func (a *App) migrateLegacyAutoStart() {
	exePath, err := currentExecutablePath()
	if err != nil || autoStartTaskMatches(exePath) {
		return
	}

	if legacyRunEntryMatches(exePath) {
		if err := createAutoStartTask(exePath); err == nil {
			_ = deleteLegacyRunEntry()
		}
	}
}

func currentExecutablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if evaluated, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = evaluated
	}
	return filepath.Clean(exePath), nil
}

func createAutoStartTask(exePath string) error {
	taskCommand := `"` + exePath + `" -hide`
	_, err := runHiddenOutput(
		"schtasks",
		"/Create",
		"/TN", autoStartTaskName,
		"/TR", taskCommand,
		"/SC", "ONLOGON",
		"/RL", "HIGHEST",
		"/F",
	)
	return err
}

func deleteAutoStartTask() error {
	if !autoStartTaskExists() {
		return nil
	}
	_, err := runHiddenOutput("schtasks", "/Delete", "/TN", autoStartTaskName, "/F")
	return err
}

func autoStartTaskExists() bool {
	_, err := runHiddenOutput("schtasks", "/Query", "/TN", autoStartTaskName)
	return err == nil
}

func autoStartTaskMatches(exePath string) bool {
	output, err := runHiddenOutput("schtasks", "/Query", "/TN", autoStartTaskName, "/FO", "LIST", "/V")
	if err != nil {
		return false
	}
	return commandReferencesExecutable(output, exePath)
}

func legacyRunEntryMatches(exePath string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	value, _, err := k.GetStringValue(autoStartRegistryName)
	if err != nil {
		return false
	}
	return commandReferencesExecutable(value, exePath)
}

func deleteLegacyRunEntry() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runRegistryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue(autoStartRegistryName)
}

func commandReferencesExecutable(command string, exePath string) bool {
	normalizedCommand := strings.ToLower(strings.ReplaceAll(command, `"`, ""))
	normalizedCommand = strings.ReplaceAll(normalizedCommand, "/", `\`)
	normalizedPath := strings.ToLower(filepath.Clean(exePath))
	return strings.Contains(normalizedCommand, normalizedPath)
}

func runHiddenOutput(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (a *App) GetLogs() string {
	return engine.GetEngineLogs()
}

func (a *App) GetAppVersion() string {
	return updater.GetVersion()
}

func (a *App) GetSettings() settings.Config {
	return settings.Get()
}

func (a *App) UpdateSettings(cfg settings.Config) string {
	previous := settings.Get()
	nextProfile := engine.ISPProfile(cfg.ISPProfile)
	needsRestart := engine.GetProfile() != nextProfile ||
		previous.SafeDNS != cfg.SafeDNS ||
		previous.NetworkTuning != cfg.NetworkTuning

	engine.SetProfile(nextProfile)
	engine.SetSystemOptimizations(cfg.SafeDNS, cfg.NetworkTuning)
	if err := settings.Save(cfg); err != nil {
		return err.Error()
	}

	if needsRestart && engine.IsProcessRunning() {
		engine.RequestRestart()
	}

	return "OK"
}

func (a *App) InstallUpdate() string {
	latest, err := updater.Check()
	if err != nil {
		return err.Error()
	}
	if latest == nil {
		return "NONE"
	}
	if err := updater.Apply(latest); err != nil {
		return err.Error()
	}
	return "OK"
}
