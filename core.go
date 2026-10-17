package main

import (
	"context"
	"embed"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/SonKoz-Game/SonKozGlide/internal/engine"
	"github.com/SonKoz-Game/SonKozGlide/internal/settings"
	"github.com/SonKoz-Game/SonKozGlide/internal/updater"
	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed resources/icon_on.ico
var iconOn []byte

//go:embed resources/icon_off.ico
var iconOff []byte

type Core struct {
	updateMenuItem *systray.MenuItem
	resources      embed.FS
	rulesYAML      []byte
	ready          chan struct{}

	mu            sync.Mutex
	pendingUpdate string
	startupError  string
	uninstalling  bool
	updating      bool
	updateApplied bool
	reconnect     bool
	bootShown     bool
	quitOnce      sync.Once

	uiMu      sync.Mutex
	ui        *uiProcess
	quitting  bool
	trimTimer *time.Timer
}

type uiProcess struct {
	cmd  *exec.Cmd
	conn *ipcConn
}

var (
	idleTrimDelay = 30 * time.Second
	trimFn        = trimWorkingSet
)

const (
	autoStartTaskName     = "SonKozGlide"
	autoStartRegistryName = "SonKozGlide"
	runRegistryPath       = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartTaskPriority = "4"
)

func NewCore(resources embed.FS, rulesYAML []byte) *Core {
	return &Core{resources: resources, rulesYAML: rulesYAML, ready: make(chan struct{})}
}

func (c *Core) init() {
	defer close(c.ready)

	if err := engine.Setup(c.resources); err != nil {
		c.mu.Lock()
		c.startupError = "Gerekli dosyalar cikartilamadi. Uygulamayi yonetici olarak calistirin. (" + err.Error() + ")"
		c.mu.Unlock()
	}
	_ = engine.ApplyRuleset(c.rulesYAML)

	conf := settings.Get()
	engine.SetProfile(engine.ISPProfile(conf.ISPProfile))
	engine.SetSystemOptimizations(conf.SafeDNS, conf.NetworkTuning)

	engine.SetStatusCallback(func(status engine.Status) {
		c.emit("service_state", status)
		c.applyTrayState(status)
	})
	engine.SetErrorCallback(func(message string) {
		c.emit("service_error", message)
	})
	engine.SetProgressCallback(func(event engine.ProgressEvent) {
		c.emit("service_progress", event)
	})

	go c.migrateLegacyAutoStart()
	go c.periodicUpdateCheck()
	go updater.RemoveLeftovers()
}

func (c *Core) run() {
	systray.Run(c.onReady, c.onExit)
	c.closeUI()
}

func (c *Core) showUI() {
	c.uiMu.Lock()
	defer c.uiMu.Unlock()
	if c.quitting {
		return
	}
	if c.ui != nil {
		allowForeground(c.ui.cmd.Process.Pid)
		c.ui.conn.notify(uiShowEvent, nil)
		return
	}
	if c.isUpdateApplied() {
		go c.RestartApp()
		return
	}

	ui, err := launchUI()
	if err != nil {
		return
	}
	c.ui = ui
	go c.serveUI(ui)
}

func launchUI() (*uiProcess, error) {
	exePath, err := currentExecutablePath()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(exePath, uiFlag)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	allowForeground(cmd.Process.Pid)
	return &uiProcess{cmd: cmd, conn: newIPCConn(stdout, stdin)}, nil
}

func (c *Core) serveUI(ui *uiProcess) {
	ui.conn.serve(func(msg ipcMessage) {
		if msg.Method == "" {
			return
		}
		go func() {
			<-c.ready
			result, err := invokeMethod(c, msg.Method, msg.Params)
			ui.conn.reply(msg.ID, result, err)
		}()
	})
	_ = ui.cmd.Wait()

	c.uiMu.Lock()
	if c.ui == ui {
		c.ui = nil
	}
	c.uiMu.Unlock()
	c.trimWhenIdle()
}

func (c *Core) trimWhenIdle() {
	c.uiMu.Lock()
	defer c.uiMu.Unlock()
	if c.trimTimer != nil {
		c.trimTimer.Stop()
	}
	c.trimTimer = time.AfterFunc(idleTrimDelay, func() {
		c.uiMu.Lock()
		idle := c.ui == nil && !c.quitting
		c.uiMu.Unlock()
		if idle {
			trimFn()
		}
	})
}

func (c *Core) uiPID() int {
	c.uiMu.Lock()
	defer c.uiMu.Unlock()
	if c.ui == nil {
		return 0
	}
	return c.ui.cmd.Process.Pid
}

func (c *Core) emit(event string, data any) {
	c.uiMu.Lock()
	ui := c.ui
	c.uiMu.Unlock()
	if ui != nil {
		ui.conn.notify(event, data)
	}
}

func (c *Core) closeUI() {
	c.uiMu.Lock()
	c.quitting = true
	ui := c.ui
	c.uiMu.Unlock()
	if ui == nil {
		return
	}
	ui.conn.notify(uiQuitEvent, nil)
	ui.conn.close()
}

func (c *Core) quit() {
	c.quitOnce.Do(func() {
		c.closeUI()
		systray.Quit()
	})
}

func (c *Core) watchShowRequests(event windows.Handle) {
	for {
		status, err := windows.WaitForSingleObject(event, windows.INFINITE)
		if err != nil || status != windows.WAIT_OBJECT_0 {
			return
		}
		c.showUI()
	}
}

func (c *Core) applyTrayState(status engine.Status) {
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

func (c *Core) periodicUpdateCheck() {
	time.Sleep(5 * time.Second)

	for {
		if settings.Get().AutoUpdate {
			if latest, err := updater.Check(); err == nil && latest != nil {
				c.mu.Lock()
				isNew := c.pendingUpdate != latest.Version()
				c.pendingUpdate = latest.Version()
				c.mu.Unlock()

				if c.updateMenuItem != nil {
					c.updateMenuItem.Show()
				}
				if isNew {
					c.emit("update_available", latest.Version())
				}
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

func (c *Core) isUpdateApplied() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.updateApplied
}

func (c *Core) GetPendingUpdate() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pendingUpdate
}

func (c *Core) onReady() {
	systray.SetIcon(iconOff)
	systray.SetTitle("SonKoz Glide")
	systray.SetTooltip("SonKoz Glide")

	c.updateMenuItem = systray.AddMenuItem("Versiyon Guncellemesi", "Yeni surum mevcut")
	c.updateMenuItem.Hide()
	systray.AddSeparator()

	mShow := systray.AddMenuItem("Arayuzu Ac", "Arayuzu ac")
	mQuit := systray.AddMenuItem("Tamamen Cikis", "Uygulamayi tamamen kapat")

	go func() {
		for {
			select {
			case <-c.updateMenuItem.ClickedCh:
				c.showUI()
				latest, _ := updater.Check()
				if latest != nil {
					c.emit("update_available", latest.Version())
				}
			case <-mShow.ClickedCh:
				c.showUI()
			case <-mQuit.ClickedCh:
				engine.Stop()
				c.quit()
			}
		}
	}()

	conf := settings.Get()
	if (conf.AutoStartBypass || c.reconnect) && !engine.IsProcessRunning() {
		engine.RequestStart()
	} else {
		go func() {
			engine.RestoreDNS()
			engine.RestoreNetworkTuning()
		}()
	}
}

func (c *Core) onExit() {
	engine.Stop()
}

func (c *Core) StartBypass() string {
	engine.RequestStart()
	return "OK"
}

func (c *Core) StopBypass() string {
	engine.RequestStop()
	return "OK"
}

func (c *Core) GetState() engine.Status {
	return engine.State()
}

func (c *Core) GetTuningReport() engine.TuningReport {
	return engine.GetTuningReport()
}

func (c *Core) GetConnectTrace() []engine.ProgressEvent {
	return engine.ConnectTrace()
}

func (c *Core) RunServiceCheck() []engine.ServiceCheck {
	return engine.CheckServices()
}

type BootReport struct {
	Info         engine.BootInfo `json:"info"`
	Version      string          `json:"version"`
	Elevated     bool            `json:"elevated"`
	AutoConnect  bool            `json:"autoConnect"`
	StartupError string          `json:"startupError"`
	Resumed      bool            `json:"resumed"`
}

func (c *Core) GetBootReport() BootReport {
	c.mu.Lock()
	startupError := c.startupError
	resumed := c.bootShown
	c.bootShown = true
	c.mu.Unlock()

	return BootReport{
		Info:         engine.GetBootInfo(),
		Version:      updater.GetVersion(),
		Elevated:     windows.GetCurrentProcessToken().IsElevated(),
		AutoConnect:  settings.Get().AutoStartBypass || c.reconnect,
		StartupError: startupError,
		Resumed:      resumed,
	}
}

func (c *Core) GetAutoStart() bool {
	exePath, err := currentExecutablePath()
	if err != nil {
		return false
	}
	return autoStartTaskMatches(exePath)
}

func (c *Core) SetAutoStart(enable bool) string {
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

func (c *Core) migrateLegacyAutoStart() {
	exePath, err := currentExecutablePath()
	if err != nil {
		return
	}
	if autoStartTaskMatches(exePath) {
		if !autoStartTaskCurrent(exePath) {
			_ = createAutoStartTask(exePath)
		}
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
	user, err := currentUserSID()
	if err != nil {
		return err
	}
	xmlPath := filepath.Join(os.TempDir(), "SonKozGlide_task.xml")
	if err := os.WriteFile(xmlPath, utf16File(autoStartTaskXML(exePath, user)), 0600); err != nil {
		return err
	}
	defer os.Remove(xmlPath)

	_, err = runHiddenOutput("schtasks", "/Create", "/TN", autoStartTaskName, "/XML", xmlPath, "/F")
	return err
}

func autoStartTaskXML(exePath string, userSID string) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <URI>\` + autoStartTaskName + `</URI>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + xmlEscape(userSID) + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + xmlEscape(userSID) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>` + autoStartTaskPriority + `</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlEscape(exePath) + `</Command>
      <Arguments>-hide</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func utf16File(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for i, u := range units {
		out[2+2*i] = byte(u)
		out[3+2*i] = byte(u >> 8)
	}
	return out
}

func currentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func autoStartTaskCurrent(exePath string) bool {
	output, err := runHiddenOutput("schtasks", "/Query", "/TN", autoStartTaskName, "/XML")
	if err != nil {
		return false
	}
	return (commandReferencesExecutable(output, exePath) || commandReferencesExecutable(output, xmlEscape(exePath))) &&
		strings.Contains(output, "<Priority>"+autoStartTaskPriority+"</Priority>") &&
		strings.Contains(output, "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>")
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

func (c *Core) GetLogs() string {
	return engine.GetEngineLogs()
}

func (c *Core) GetAppVersion() string {
	return updater.GetVersion()
}

func (c *Core) GetSettings() settings.Config {
	return settings.Get()
}

func (c *Core) UpdateSettings(cfg settings.Config) string {
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

func (c *Core) InstallUpdate() string {
	c.mu.Lock()
	if c.updating {
		c.mu.Unlock()
		return "BUSY"
	}
	if c.updateApplied {
		c.mu.Unlock()
		return "OK"
	}
	c.updating = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.updating = false
		c.mu.Unlock()
	}()

	latest, err := updater.Check()
	if err != nil {
		return err.Error()
	}
	if latest == nil {
		return "NONE"
	}
	err = updater.Apply(latest, func(p updater.Progress) {
		c.emit("update_progress", p)
	})
	if err != nil {
		return err.Error()
	}

	c.mu.Lock()
	c.updateApplied = true
	c.mu.Unlock()
	return "OK"
}

func (c *Core) RestartApp() string {
	exePath, err := currentExecutablePath()
	if err != nil {
		return err.Error()
	}

	args := []string{restartFlag}
	if engine.IsProcessRunning() {
		args = append(args, reconnectFlag)
	}
	if err := exec.Command(exePath, args...).Start(); err != nil {
		return err.Error()
	}

	go func() {
		engine.Stop()
		c.quit()
	}()
	return "OK"
}
