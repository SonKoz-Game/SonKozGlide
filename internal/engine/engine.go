package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

type ISPProfile string

const (
	ISPAuto        ISPProfile = "auto"
	ISPTurkTelekom ISPProfile = "turktelekom"
	ISPVodafone    ISPProfile = "vodafone"
	ISPSuperonline ISPProfile = "superonline"
	ISPGeneric     ISPProfile = "generic"
)

const TargetDirName = "SonKozGlide"

const (
	startupStableWindow   = 500 * time.Millisecond
	restartBaseDelay      = 2 * time.Second
	restartMaxDelay       = 30 * time.Second
	crashRestartDelay     = 250 * time.Millisecond
	crashRestartMinUptime = 30 * time.Second
	healthInitialDelay    = 15 * time.Second
	healthInterval        = 15 * time.Second
	healthProbeTimeout    = 5 * time.Second
	healthRestartAfter    = 3
	healthRestartDelay    = 300 * time.Millisecond
	shortProbeSetSize     = 4
	probeAddressAttempts  = 2
	maxLogFileBytes       = 1024 * 1024
	maxLogReadBytes       = 64 * 1024
	adaptiveStateFile     = "adaptive_profiles.json"
	adaptiveStateVer      = 2
	adaptiveAlpha         = 0.35
	adaptiveFreshWindow   = 14 * 24 * time.Hour
	dohLookupTimeout      = 2 * time.Second
	dohDialTimeout        = 2 * time.Second
	dnsWarmTimeout        = 3 * time.Second
	probeWarmWait         = 1500 * time.Millisecond
	dnsCacheTTL           = 5 * time.Minute
	controlProbeTimeout   = 2500 * time.Millisecond
	offlineLogInterval    = 2 * time.Minute
	restartNotifyAfter    = 3
)

type healthState int

const (
	healthOK healthState = iota
	healthInconclusive
	healthUnhealthy
	healthOffline
)

var healthTargets = []string{
	"discord.com",
	"gateway.discord.gg",
	"discordapp.com",
	"cdn.discordapp.com",
	"media.discordapp.net",
	"roblox.com",
	"apis.roblox.com",
	"tr.rbxcdn.com",
}

var controlTargets = []string{
	"www.google.com",
	"www.microsoft.com",
	"www.cloudflare.com",
}

var probeFn = probeTargetWithTimeout

var dohResolverURLs = []string{
	"https://1.1.1.1/dns-query?type=A&name=",
	"https://8.8.8.8/resolve?type=A&name=",
}

var dohClient = &http.Client{
	Transport: &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: dohDialTimeout}).DialContext,
		TLSHandshakeTimeout: dohDialTimeout,
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
	},
}

var (
	mu                        sync.Mutex
	adaptiveMu                sync.Mutex
	dnsCacheMu                sync.Mutex
	winwsProcess              *exec.Cmd
	processDone               chan struct{}
	isIntentionalStop         bool
	autoRestart               bool
	restartFailures           int
	currentLogFile            *os.File
	currentProfile            ISPProfile = ISPAuto
	activeStrategy            string
	currentPhase              Phase = PhaseIdle
	phaseDetail               string
	connectionHealthy         bool
	healthTriggeredRestart    bool
	healthCheckCancel         context.CancelFunc
	serviceGeneration         uint64
	statusCallback            func(status Status)
	errorCallback             func(message string)
	adaptiveStatePathOverride string
	dnsCache                  = make(map[string]dnsCacheEntry)
	safeDNSEnabled            = true
	networkTuningEnabled      = true
)

type probeMetric struct {
	Target  string `json:"target"`
	Latency int64  `json:"latency"`
	Status  string `json:"status"`
}

type dnsCacheEntry struct {
	ips       []net.IP
	expiresAt time.Time
}

type adaptiveProfileStats struct {
	Samples         int       `json:"samples"`
	Selections      int       `json:"selections"`
	Successes       int       `json:"successes"`
	Failures        int       `json:"failures"`
	StartupFailures int       `json:"startupFailures"`
	ProcessFailures int       `json:"processFailures"`
	SuccessEWMA     float64   `json:"successEwma"`
	LatencyEWMA     float64   `json:"latencyEwmaMs"`
	StabilityEWMA   float64   `json:"stabilityEwma"`
	LastSelectedAt  time.Time `json:"lastSelectedAt,omitempty"`
	LastSuccessAt   time.Time `json:"lastSuccessAt,omitempty"`
	LastFailureAt   time.Time `json:"lastFailureAt,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
}

type adaptiveState struct {
	Version      int                             `json:"version"`
	LastProfile  string                          `json:"lastProfile,omitempty"`
	LastUpdated  time.Time                       `json:"lastUpdated,omitempty"`
	ProfileStats map[string]adaptiveProfileStats `json:"profileStats"`
}

func SetStatusCallback(cb func(status Status)) {
	mu.Lock()
	statusCallback = cb
	mu.Unlock()
}

func SetErrorCallback(cb func(message string)) {
	mu.Lock()
	errorCallback = cb
	mu.Unlock()
}

func getTargetDir() string {
	programData := os.Getenv("PROGRAMDATA")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, TargetDirName)
}

func SetProfile(profile ISPProfile) {
	mu.Lock()
	defer mu.Unlock()
	currentProfile = profile
}

func GetProfile() ISPProfile {
	mu.Lock()
	defer mu.Unlock()
	return currentProfile
}

func SetSystemOptimizations(safeDNS bool, networkTuning bool) {
	mu.Lock()
	safeDNSEnabled = safeDNS
	networkTuningEnabled = networkTuning
	mu.Unlock()
}

func systemOptimizations() (bool, bool) {
	mu.Lock()
	defer mu.Unlock()
	return safeDNSEnabled, networkTuningEnabled
}

func adaptiveProfileScore(stats adaptiveProfileStats, hasStats bool, order int, now time.Time, lastSelected bool) float64 {
	orderPenalty := float64(order) * 10
	if !hasStats || stats.Samples == 0 {
		score := 2000 + orderPenalty
		if lastSelected {
			score -= 15
		}
		return score
	}

	success := clamp01(stats.SuccessEWMA)
	stability := clamp01(stats.StabilityEWMA)
	latency := stats.LatencyEWMA
	if latency <= 0 {
		latency = float64(healthProbeTimeout.Milliseconds())
	}

	score := latency + (1-success)*2000 + (1-stability)*1250 + orderPenalty
	if stats.LastSuccessAt.IsZero() {
		score += 350
	} else if now.Sub(stats.LastSuccessAt) > adaptiveFreshWindow {
		score += 300
	}
	if stats.LastFailureAt.After(stats.LastSuccessAt) {
		score += 250
	}

	startupFailures := stats.StartupFailures
	if startupFailures > 5 {
		startupFailures = 5
	}
	processFailures := stats.ProcessFailures
	if processFailures > 5 {
		processFailures = 5
	}
	score += float64(startupFailures)*60 + float64(processFailures)*40

	if lastSelected {
		score -= 15
	}
	return score
}

func recordProfileSelected(profile string) {
	if !isKnownStrategy(profile) {
		return
	}

	adaptiveMu.Lock()
	defer adaptiveMu.Unlock()

	state := loadAdaptiveStateLocked()
	stats := state.ProfileStats[profile]
	now := time.Now()
	stats.Selections++
	stats.LastSelectedAt = now
	state.LastProfile = profile
	state.LastUpdated = now
	state.ProfileStats[profile] = stats
	saveAdaptiveStateLocked(state)
}

func recordProfileProbe(profile string, metrics []probeMetric, probeState healthState) {
	if !isKnownStrategy(profile) || len(metrics) == 0 {
		return
	}

	successes := 0
	var latencyTotal int64
	for _, metric := range metrics {
		if isProbeOK(metric.Status) {
			successes++
			latencyTotal += metric.Latency
		}
	}

	latency := float64(healthProbeTimeout.Milliseconds())
	if successes > 0 {
		latency = float64(latencyTotal) / float64(successes)
	}

	successRatio := float64(successes) / float64(len(metrics))
	stabilitySample := 0.0
	switch probeState {
	case healthOK:
		stabilitySample = 1.0
	case healthInconclusive:
		stabilitySample = 0.35
	}

	adaptiveMu.Lock()
	defer adaptiveMu.Unlock()

	state := loadAdaptiveStateLocked()
	stats := state.ProfileStats[profile]
	stats.Samples++
	stats.Successes += successes
	stats.Failures += len(metrics) - successes
	stats.SuccessEWMA = adaptiveEWMA(stats.SuccessEWMA, successRatio, stats.Samples)
	stats.LatencyEWMA = adaptiveEWMA(stats.LatencyEWMA, latency, stats.Samples)
	stats.StabilityEWMA = adaptiveEWMA(stats.StabilityEWMA, stabilitySample, stats.Samples)
	now := time.Now()
	if probeState == healthOK {
		stats.LastSuccessAt = now
		stats.LastError = ""
		state.LastProfile = profile
	} else if probeState == healthInconclusive {
		stats.LastError = "health check inconclusive"
	} else {
		stats.LastFailureAt = now
		stats.LastError = "health check failed"
	}
	state.LastUpdated = now
	state.ProfileStats[profile] = stats
	saveAdaptiveStateLocked(state)
}

func recordProfileFailure(profile string, reason string, startupFailure bool, processFailure bool) {
	if !isKnownStrategy(profile) {
		return
	}

	adaptiveMu.Lock()
	defer adaptiveMu.Unlock()

	state := loadAdaptiveStateLocked()
	stats := state.ProfileStats[profile]
	stats.Samples++
	stats.Failures++
	if startupFailure {
		stats.StartupFailures++
	}
	if processFailure {
		stats.ProcessFailures++
	}
	stats.SuccessEWMA = adaptiveEWMA(stats.SuccessEWMA, 0, stats.Samples)
	stats.LatencyEWMA = adaptiveEWMA(stats.LatencyEWMA, float64(healthProbeTimeout.Milliseconds()*2), stats.Samples)
	stats.StabilityEWMA = adaptiveEWMA(stats.StabilityEWMA, 0, stats.Samples)
	now := time.Now()
	stats.LastFailureAt = now
	stats.LastError = truncateReason(reason)
	state.LastUpdated = now
	state.ProfileStats[profile] = stats
	saveAdaptiveStateLocked(state)
}

func adaptiveEWMA(current float64, sample float64, samples int) float64 {
	if samples <= 1 || current <= 0 {
		return sample
	}
	return current*(1-adaptiveAlpha) + sample*adaptiveAlpha
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func truncateReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= 240 {
		return reason
	}
	return reason[:240]
}

func adaptiveStatePath() string {
	if adaptiveStatePathOverride != "" {
		return adaptiveStatePathOverride
	}
	return filepath.Join(getTargetDir(), adaptiveStateFile)
}

func defaultAdaptiveState() adaptiveState {
	return adaptiveState{
		Version:      adaptiveStateVer,
		ProfileStats: make(map[string]adaptiveProfileStats),
	}
}

func loadAdaptiveStateLocked() adaptiveState {
	data, err := os.ReadFile(adaptiveStatePath())
	if err != nil {
		return defaultAdaptiveState()
	}

	var state adaptiveState
	if err := json.Unmarshal(data, &state); err != nil || state.Version != adaptiveStateVer {
		return defaultAdaptiveState()
	}
	if state.ProfileStats == nil {
		state.ProfileStats = make(map[string]adaptiveProfileStats)
	}
	return state
}

func saveAdaptiveStateLocked(state adaptiveState) {
	state.Version = adaptiveStateVer
	if state.ProfileStats == nil {
		state.ProfileStats = make(map[string]adaptiveProfileStats)
	}

	path := adaptiveStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.WriteFile(path, data, 0644)
		_ = os.Remove(tmpPath)
	}
}

func IsProcessRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return isActivePhase(currentPhase)
}

func Setup(embeddedFiles embed.FS) error {
	started := time.Now()
	targetDir := getTargetDir()
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	pathPtr, _ := syscall.UTF16PtrFromString(targetDir)
	_ = syscall.SetFileAttributes(pathPtr, syscall.FILE_ATTRIBUTE_HIDDEN)

	files, err := fs.ReadDir(embeddedFiles, "resources")
	if err != nil {
		return err
	}

	updated := 0
	components := make([]BootFile, 0, len(files))
	for _, file := range files {
		data, err := embeddedFiles.ReadFile("resources/" + file.Name())
		if err != nil {
			continue
		}

		destPath := filepath.Join(targetDir, file.Name())
		if stat, err := os.Stat(destPath); err == nil && stat.Size() == int64(len(data)) {
			existing, readErr := os.ReadFile(destPath)
			if readErr == nil && bytes.Equal(existing, data) {
				components = append(components, BootFile{Name: file.Name(), Bytes: int64(len(data))})
				continue
			}
		}

		if err := os.WriteFile(destPath, data, 0755); err != nil {
			CleanOldServices()
			if retryErr := os.WriteFile(destPath, data, 0755); retryErr != nil {
				return fmt.Errorf("%s yazilamadi: %w", file.Name(), retryErr)
			}
		}
		updated++
		components = append(components, BootFile{Name: file.Name(), Bytes: int64(len(data)), Updated: true})
	}
	_ = os.Remove(filepath.Join(targetDir, legacyBrokerExe))

	recordSetupStats(components, updated, time.Since(started))
	return nil
}

func Start() error {
	mu.Lock()
	if isProcessAlive(winwsProcess) {
		isIntentionalStop = false
		autoRestart = true
		healthy := connectionHealthy
		mu.Unlock()

		if healthy {
			setPhase(PhaseRunning, "")
		} else {
			setPhase(PhaseStarting, "baglanti dogrulaniyor")
		}
		return nil
	}

	isIntentionalStop = false
	autoRestart = true
	restartFailures = 0
	requestedProfile := currentProfile
	mu.Unlock()

	ctx, release := newScanContext()
	defer release()

	run := beginProgressRun("connect")
	step(run, "prepare", stepActive, "")
	warmStarted := time.Now()
	warmed := make(chan struct{})
	go func() {
		warmProbeDNS()
		close(warmed)
	}()
	CleanOldServices()
	startSystemOptimizations(run)
	select {
	case <-warmed:
	case <-time.After(probeWarmWait):
	}
	appendEngineLog(fmt.Sprintf("probe dns warm: %d/%d resolved in %dms", resolvedProbeTargets(), len(healthTargets), time.Since(warmStarted).Milliseconds()))
	step(run, "prepare", stepDone, "")

	order := strategyOrder(preferredStrategy(requestedProfile), "")
	if err := connectStrategies(ctx, run, PhaseStarting, order, nil); err != nil {
		return err
	}
	go reportPaymentServices(run)
	return nil
}

func startSystemOptimizations(run uint64) {
	safeDNS, networkTuning := systemOptimizations()
	setDNSWanted(safeDNS)
	setTuningWanted(networkTuning)

	go func() {
		if !safeDNS {
			restoreDNSIfUnwanted()
			step(run, "dns", stepSkipped, "off")
			return
		}
		step(run, "dns", stepActive, "")
		ensureEncryptedDNS()
		state, detail := dnsStepResult()
		step(run, "dns", state, detail)
	}()
	go func() {
		if !networkTuning {
			restoreTuningIfUnwanted()
			step(run, "tuning", stepSkipped, "off")
			return
		}
		step(run, "tuning", stepActive, "")
		ensureNetworkTuning()
		state, detail := tuningStepResult()
		step(run, "tuning", state, detail)
	}()
}

func restoreSystemOptimizations() {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		RestoreDNS()
	}()
	go func() {
		defer wg.Done()
		RestoreNetworkTuning()
	}()
	wg.Wait()
}

func resolvedProbeTargets() int {
	resolved := 0
	for _, target := range healthTargets {
		if _, ok := cachedDNS(target); ok {
			resolved++
		}
	}
	return resolved
}

func warmProbeDNS() {
	ctx, cancel := context.WithTimeout(context.Background(), dnsWarmTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, target := range healthTargets {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			resolveProbeIPs(ctx, host)
		}(target)
	}
	wg.Wait()
}

func monitorService(p *brokerProc, generation uint64) {
	<-p.done

	duration := time.Since(p.started)
	current := p.strategy.ID

	mu.Lock()
	isCurrentProcess := winwsProcess == p.cmd
	shouldRestart := isCurrentProcess && !isIntentionalStop && autoRestart
	wasConnectionHealthy := connectionHealthy
	wasHealthRestart := false
	requestedProfile := currentProfile
	if isCurrentProcess {
		wasHealthRestart = healthTriggeredRestart
		healthTriggeredRestart = false
		winwsProcess = nil
		processDone = nil
		connectionHealthy = false
		if healthCheckCancel != nil {
			healthCheckCancel()
			healthCheckCancel = nil
		}
	}
	if currentLogFile == p.logFile {
		currentLogFile = nil
	}

	failures := restartFailures
	if shouldRestart {
		if wasHealthRestart {
			restartFailures = 0
		} else if duration < 10*time.Second || !wasConnectionHealthy {
			restartFailures++
		} else {
			restartFailures = 0
		}
		failures = restartFailures
	}
	mu.Unlock()

	if p.logFile != nil {
		_ = p.logFile.Close()
	}

	if !shouldRestart {
		return
	}

	firstDelay := restartDelay(failures)
	demoted := ""
	if wasHealthRestart {
		recordProfileFailure(current, "health recovery cycle", false, false)
		firstDelay = healthRestartDelay
	} else {
		recordProfileFailure(current, fmt.Sprintf("process exited after %s", duration.Round(time.Second)), false, true)
		if wasConnectionHealthy && duration >= crashRestartMinUptime {
			firstDelay = crashRestartDelay
		}
		if duration < 30*time.Second || !wasConnectionHealthy {
			demoted = current
		}
	}

	setPhase(PhaseRecovering, "baglanti yeniden kuruluyor")
	restartServiceUntilStopped(requestedProfile, current, demoted, failures, generation, firstDelay)
}

func restartServiceUntilStopped(requestedProfile ISPProfile, current string, demoted string, failures int, generation uint64, firstDelay time.Duration) {
	delay := firstDelay
	notified := false
	preferred := current
	if preferred == demoted {
		preferred = preferredStrategy(requestedProfile)
	}
	for {
		time.Sleep(delay)

		if !shouldAutoRestartGeneration(generation) {
			return
		}

		err := reconnect(preferred, demoted, generation)
		if err == nil || errors.Is(err, errScanCanceled) {
			return
		}

		appendEngineLog(fmt.Sprintf("restart failed: %v; retrying", err))

		if !notified && failures >= restartNotifyAfter {
			notified = true
			notifyError(err.Error())
		}

		mu.Lock()
		if serviceGeneration != generation || isIntentionalStop || !autoRestart {
			mu.Unlock()
			return
		}
		restartFailures++
		failures = restartFailures
		mu.Unlock()

		delay = restartDelay(failures)
	}
}

func reconnect(preferred string, demoted string, generation uint64) error {
	ctx, release := newScanContext()
	defer release()

	run := beginProgressRun("recover")
	warmProbeDNS()
	admit := func() bool {
		return !isIntentionalStop && autoRestart && serviceGeneration == generation
	}
	return connectStrategies(ctx, run, PhaseRecovering, strategyOrder(preferred, demoted), admit)
}

func setPhaseForProcess(cmd *exec.Cmd, phase Phase, detail string) {
	mu.Lock()
	current := winwsProcess == cmd && !isIntentionalStop
	mu.Unlock()

	if current {
		setPhase(phase, detail)
	}
}

func healthCheckLoop(ctx context.Context, cmd *exec.Cmd, strategy string, firstCheck time.Duration) {
	if !sleepContext(ctx, firstCheck) {
		return
	}

	lastLoggedIssue := ""
	lastIssueLogAt := time.Time{}
	lastOfflineLogAt := time.Time{}
	consecutiveUnhealthy := 0
	for {
		refreshEncryptedDNSIfNetworkChanged()

		metrics, state, reason := runHealthProbe(ctx, strategy, healthProbeTimeout)
		if len(metrics) == 0 {
			return
		}
		if state == healthOffline {
			consecutiveUnhealthy = 0
			lastLoggedIssue = ""
			if time.Since(lastOfflineLogAt) >= offlineLogInterval {
				appendEngineLog("waiting for internet connection; keeping service running")
				lastOfflineLogAt = time.Now()
			}
			markConnectionUnverified(cmd)
			setPhaseForProcess(cmd, PhaseRecovering, "internet baglantisi bekleniyor")
			if !sleepContext(ctx, healthInterval) {
				return
			}
			continue
		}
		if state == healthOK {
			lastLoggedIssue = ""
			consecutiveUnhealthy = 0
			markConnectionHealthy(cmd)
		} else {
			if state == healthUnhealthy {
				consecutiveUnhealthy++
			}
			if shouldLogHealthIssue(reason, lastLoggedIssue, lastIssueLogAt) {
				appendEngineLog(fmt.Sprintf("%s for strategy=%s; unhealthy_count=%d/%d", reason, strategy, consecutiveUnhealthy, healthRestartAfter))
				lastLoggedIssue = reason
				lastIssueLogAt = time.Now()
			}
			if consecutiveUnhealthy >= healthRestartAfter && !inFallbackCooldown() {
				restartUnhealthyService(cmd, reason)
				return
			}
		}

		if !sleepContext(ctx, healthInterval) {
			return
		}
	}
}

func inFallbackCooldown() bool {
	mu.Lock()
	defer mu.Unlock()
	return time.Now().Before(fallbackUntil)
}

func runHealthProbe(ctx context.Context, profile string, timeout time.Duration) ([]probeMetric, healthState, string) {
	metrics := probeTargetList(ctx, healthTargets, timeout)
	if len(metrics) == 0 {
		return nil, healthInconclusive, ""
	}

	state, reason := assessHealth(metrics)

	if state != healthOK && !internetReachable(ctx) {
		return metrics, healthOffline, "internet baglantisi yok"
	}

	recordProfileProbe(profile, metrics, state)
	return metrics, state, reason
}

func internetReachable(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, controlProbeTimeout)
	defer cancel()

	results := make(chan bool, len(controlTargets))
	for _, target := range controlTargets {
		go func(host string) {
			conn, err := dialTLS(probeCtx, host, host, controlProbeTimeout)
			if err == nil {
				_ = conn.Close()
			}
			results <- err == nil
		}(target)
	}

	for range controlTargets {
		select {
		case ok := <-results:
			if ok {
				return true
			}
		case <-probeCtx.Done():
			return false
		}
	}
	return false
}

func probeTargetList(ctx context.Context, targets []string, timeout time.Duration) []probeMetric {
	return probeTargets(ctx, targets, timeout, true)
}

func probeTargets(ctx context.Context, targets []string, timeout time.Duration, stopWhenHealthy bool) []probeMetric {
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan probeMetric, len(targets))
	for _, target := range targets {
		go func(host string) {
			results <- probeFn(probeCtx, host, timeout)
		}(target)
	}

	metrics := make([]probeMetric, 0, len(targets))
	for range targets {
		metric := <-results
		if ctx.Err() != nil {
			return nil
		}
		metrics = append(metrics, metric)
		if stopWhenHealthy && countHealth(metrics).confirmsHealthy(len(targets)) {
			return metrics
		}
	}
	return metrics
}

func probeTargetWithTimeout(ctx context.Context, target string, timeout time.Duration) probeMetric {
	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if ips, ok := resolveProbeIPs(probeCtx, target); ok {
		var err error
		for i, ip := range ips {
			if i >= probeAddressAttempts || probeCtx.Err() != nil {
				break
			}
			conn, dialErr := dialTLS(probeCtx, target, ip.String(), remainingTimeout(start, timeout))
			if dialErr == nil {
				_ = conn.Close()
				return probeMetric{Target: target, Latency: time.Since(start).Milliseconds(), Status: "ok"}
			}
			err = dialErr
			if status := classifyProbeError(dialErr); status == "timeout" || status == "reset" {
				break
			}
		}
		return probeMetric{Target: target, Latency: time.Since(start).Milliseconds(), Status: classifyProbeError(err)}
	}

	conn, err := dialTLS(probeCtx, target, target, remainingTimeout(start, timeout))
	latency := time.Since(start).Milliseconds()
	if err == nil {
		_ = conn.Close()
		return probeMetric{Target: target, Latency: latency, Status: "ok_dns_fallback"}
	}
	return probeMetric{Target: target, Latency: latency, Status: classifyProbeError(err)}
}

func dialTLS(ctx context.Context, serverName string, address string, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		timeout = 250 * time.Millisecond
	}

	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config:    &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12},
	}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, "443"))
}

func remainingTimeout(start time.Time, total time.Duration) time.Duration {
	remaining := total - time.Since(start)
	if remaining < 250*time.Millisecond {
		return 250 * time.Millisecond
	}
	return remaining
}

func resolveProbeIPs(ctx context.Context, target string) ([]net.IP, bool) {
	if ips, ok := cachedDNS(target); ok {
		return ips, true
	}

	lookupCtx, cancel := context.WithTimeout(ctx, dohLookupTimeout)
	defer cancel()

	results := make(chan []net.IP, len(dohResolverURLs))
	for _, base := range dohResolverURLs {
		go func(endpoint string) {
			results <- lookupDoH(lookupCtx, endpoint)
		}(base + url.QueryEscape(target))
	}

	for range dohResolverURLs {
		select {
		case ips := <-results:
			if len(ips) > 0 {
				cacheDNS(target, ips)
				return ips, true
			}
		case <-lookupCtx.Done():
			return nil, false
		}
	}
	return nil, false
}

type dohResponse struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func lookupDoH(ctx context.Context, endpoint string) []net.IP {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/dns-json")

	resp, err := dohClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var answer dohResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&answer); err != nil {
		return nil
	}
	return parseDoHAnswer(answer)
}

func parseDoHAnswer(answer dohResponse) []net.IP {
	if answer.Status != 0 {
		return nil
	}

	seen := make(map[string]bool)
	var ips []net.IP
	for _, record := range answer.Answer {
		if record.Type != 1 {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(record.Data)).To4()
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		ips = append(ips, ip)
	}
	return ips
}

func cachedDNS(target string) ([]net.IP, bool) {
	dnsCacheMu.Lock()
	defer dnsCacheMu.Unlock()

	entry, ok := dnsCache[target]
	if !ok || time.Now().After(entry.expiresAt) || len(entry.ips) == 0 {
		return nil, false
	}

	ips := make([]net.IP, len(entry.ips))
	copy(ips, entry.ips)
	return ips, true
}

func cacheDNS(target string, ips []net.IP) {
	if len(ips) == 0 {
		return
	}

	copied := make([]net.IP, len(ips))
	copy(copied, ips)

	dnsCacheMu.Lock()
	dnsCache[target] = dnsCacheEntry{ips: copied, expiresAt: time.Now().Add(dnsCacheTTL)}
	dnsCacheMu.Unlock()
}

func classifyProbeError(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns_failed"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}

	if errors.Is(err, windows.WSAECONNRESET) || errors.Is(err, windows.WSAECONNABORTED) || errors.Is(err, syscall.ECONNRESET) {
		return "reset"
	}

	return "failed"
}

type healthCounts struct {
	total                 int
	successes             int
	discordSuccesses      int
	discordCoreSuccesses  int
	discordMediaSuccesses int
	dnsFailures           int
	hardFailures          int
	discordHardFailures   int
}

func countHealth(metrics []probeMetric) healthCounts {
	counts := healthCounts{total: len(metrics)}
	for _, metric := range metrics {
		if isProbeOK(metric.Status) {
			counts.successes++
			if isDiscordTarget(metric.Target) {
				counts.discordSuccesses++
			}
			if isDiscordCoreTarget(metric.Target) {
				counts.discordCoreSuccesses++
			}
			if isDiscordMediaTarget(metric.Target) {
				counts.discordMediaSuccesses++
			}
			continue
		}

		if metric.Status == "dns_failed" {
			counts.dnsFailures++
			continue
		}

		counts.hardFailures++
		if isDiscordTarget(metric.Target) {
			counts.discordHardFailures++
		}
	}
	return counts
}

func (c healthCounts) confirmsHealthy(targets int) bool {
	return c.hardFailures == 0 && c.dnsFailures == 0 && c.successes >= 2 && c.successes >= targets-1
}

func tolerableFailures(resolved int) int {
	return resolved / 8
}

func assessHealth(metrics []probeMetric) (healthState, string) {
	counts := countHealth(metrics)
	resolved := counts.successes + counts.hardFailures
	summary := formatHealthSummary(metrics)

	if counts.successes == 0 && counts.dnsFailures > 0 {
		return healthUnhealthy, "health check failed: dns resolution failed; " + summary
	}

	if (counts.total-counts.dnsFailures)*2 <= counts.total {
		return healthInconclusive, "health check inconclusive: dns resolution failed; " + summary
	}

	discordReady := shortProbeSet(metrics) || (counts.discordCoreSuccesses > 0 && counts.discordMediaSuccesses > 0)
	if counts.hardFailures <= tolerableFailures(resolved) && counts.successes >= 2 && counts.discordSuccesses > 0 && discordReady {
		return healthOK, ""
	}

	if counts.hardFailures == 0 {
		return healthInconclusive, "health check inconclusive: " + summary
	}

	if counts.hardFailures*4 > resolved {
		return healthUnhealthy, "health check failed: " + summary
	}

	return healthInconclusive, "health check degraded: " + summary
}

func isProbeOK(status string) bool {
	return status == "ok" || status == "ok_dns_fallback"
}

func shouldLogHealthIssue(reason string, lastReason string, lastLoggedAt time.Time) bool {
	return reason != lastReason || time.Since(lastLoggedAt) >= time.Minute
}

func shortProbeSet(metrics []probeMetric) bool {
	return len(metrics) <= shortProbeSetSize
}

func isDiscordTarget(target string) bool {
	return strings.Contains(target, "discord")
}

func isDiscordCoreTarget(target string) bool {
	return target == "discord.com" ||
		target == "api.discord.com" ||
		strings.Contains(target, "gateway.discord")
}

func isDiscordMediaTarget(target string) bool {
	return strings.Contains(target, "discordapp") ||
		strings.Contains(target, "discordcdn") ||
		strings.Contains(target, "discord.media")
}

func formatHealthSummary(metrics []probeMetric) string {
	parts := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		parts = append(parts, fmt.Sprintf("%s=%s(%dms)", metric.Target, metric.Status, metric.Latency))
	}
	return strings.Join(parts, ", ")
}

func markConnectionHealthy(cmd *exec.Cmd) {
	mu.Lock()
	if winwsProcess != cmd {
		mu.Unlock()
		return
	}
	alreadyHealthy := connectionHealthy && currentPhase == PhaseRunning && phaseDetail == ""
	connectionHealthy = true
	fallbackUntil = time.Time{}
	mu.Unlock()

	if alreadyHealthy {
		return
	}
	setPhase(PhaseRunning, "")
}

func markConnectionUnverified(cmd *exec.Cmd) {
	mu.Lock()
	if winwsProcess == cmd {
		connectionHealthy = false
	}
	mu.Unlock()
}

func restartUnhealthyService(cmd *exec.Cmd, reason string) {
	mu.Lock()
	if winwsProcess != cmd || isIntentionalStop || !autoRestart {
		mu.Unlock()
		return
	}
	connectionHealthy = false
	healthTriggeredRestart = true
	mu.Unlock()

	appendEngineLog("health recovery restarting service: " + reason)
	setPhase(PhaseRecovering, "baglanti tazeleniyor")
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func restartDelay(failures int) time.Duration {
	if failures <= 0 {
		return restartBaseDelay
	}

	delay := restartBaseDelay
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= restartMaxDelay {
			return restartMaxDelay
		}
	}
	return delay
}

func shouldAutoRestartGeneration(generation uint64) bool {
	mu.Lock()
	defer mu.Unlock()
	return !isIntentionalStop && autoRestart && serviceGeneration == generation
}

func openServiceLog(logPath string) (*os.File, error) {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if stat, err := os.Stat(logPath); err == nil && stat.Size() > maxLogFileBytes {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	return os.OpenFile(logPath, flags, 0644)
}

func appendEngineLog(line string) {
	logPath := filepath.Join(getTargetDir(), "sk_service.log")
	logFile, err := openServiceLog(logPath)
	if err != nil {
		return
	}
	defer logFile.Close()

	_, _ = fmt.Fprintf(logFile, "[%s] %s\n", time.Now().Format(time.RFC3339), line)
}

func Stop() {
	setDNSWanted(false)
	setTuningWanted(false)
	haltService()
	restoreSystemOptimizations()
}

func haltService() {
	cancelScans()

	mu.Lock()
	isIntentionalStop = true
	autoRestart = false
	connectionHealthy = false
	healthTriggeredRestart = false
	activeStrategy = ""
	fallbackUntil = time.Time{}
	proc := winwsProcess
	done := processDone
	cancel := healthCheckCancel
	healthCheckCancel = nil
	serviceGeneration++
	mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
		if done != nil {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
		}
	}

	mu.Lock()
	if winwsProcess == proc {
		winwsProcess = nil
		processDone = nil
	}
	currentLogFile = nil
	mu.Unlock()

	CleanOldServices()
}

func GetEngineLogs() string {
	logPath := filepath.Join(getTargetDir(), "sk_service.log")
	data, err := readLogTail(logPath, maxLogReadBytes)
	if err != nil {
		return ""
	}

	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || isRawArgumentLine(line) {
			continue
		}
		lines = append(lines, line)
	}

	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
	}
	return strings.Join(lines, "\n")
}

func isRawArgumentLine(line string) bool {
	return strings.Contains(line, "--dpi-desync") ||
		strings.Contains(line, "--hostlist") ||
		strings.Contains(line, "--filter-") ||
		strings.Contains(line, "--wf-")
}

func readLogTail(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	start := int64(0)
	if stat.Size() > maxBytes {
		start = stat.Size() - maxBytes
	}

	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	if start > 0 {
		if idx := bytes.IndexByte(data, '\n'); idx >= 0 && idx+1 < len(data) {
			data = data[idx+1:]
		}
	}

	return data, nil
}
