package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	verifyRounds           = 2
	verifyProbeTimeout     = 3 * time.Second
	verifyMaxFailures      = 1
	verifyMinSuccesses     = 6
	launchFailureLimit     = 2
	fallbackCooldown       = 10 * time.Minute
	serviceProbeTime       = 4 * time.Second
	unverifiedRecheckDelay = 3 * time.Second
)

var errScanCanceled = errors.New("baglanti taramasi iptal edildi")

var paymentTargets = []string{
	"discord.com",
	"js.stripe.com",
	"api.stripe.com",
	"hcaptcha.com",
	"apis.roblox.com",
	"roblox-api.arkoselabs.com",
	"www.paypal.com",
}

var (
	scanMu        sync.Mutex
	scanSeq       uint64
	scanCancels   = map[uint64]context.CancelFunc{}
	connectMu     sync.Mutex
	fallbackUntil time.Time
	launchFn      = launchBroker
	verifyFn      = verifyStrategy
)

type brokerProc struct {
	cmd      *exec.Cmd
	done     chan struct{}
	logFile  *os.File
	started  time.Time
	strategy Strategy
}

func (p *brokerProc) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *brokerProc) stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
		}
	}
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
}

type verifyResult struct {
	metrics  []probeMetric
	state    healthState
	ok       int
	count    int
	latency  int64
	canceled bool
	partial  bool
}

type ServiceCheck struct {
	ID      string   `json:"id"`
	OK      int      `json:"ok"`
	Count   int      `json:"count"`
	Latency int64    `json:"latency"`
	Failed  []string `json:"failed"`
}

func newScanContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	scanMu.Lock()
	scanSeq++
	id := scanSeq
	scanCancels[id] = cancel
	scanMu.Unlock()

	return ctx, func() {
		scanMu.Lock()
		delete(scanCancels, id)
		scanMu.Unlock()
		cancel()
	}
}

func cancelScans() {
	scanMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(scanCancels))
	for _, cancel := range scanCancels {
		cancels = append(cancels, cancel)
	}
	scanMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func launchBroker(s Strategy) (*brokerProc, error) {
	targetDir := getTargetDir()
	cmd := hideWindow(exec.Command(filepath.Join(targetDir, brokerExe), buildArgs(s)...))
	cmd.Dir = targetDir

	logFile, err := openServiceLog(filepath.Join(targetDir, "sk_service.log"))
	if err == nil {
		_, _ = fmt.Fprintf(logFile, "\n[%s] starting strategy=%s\n", time.Now().Format(time.RFC3339), s.ID)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	} else {
		logFile = nil
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return nil, err
	}

	p := &brokerProc{cmd: cmd, done: make(chan struct{}), logFile: logFile, started: time.Now(), strategy: s}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()

	if !waitForProcessStable(cmd, startupStableWindow) {
		p.stop()
		return nil, fmt.Errorf("servis baslatilamadi (surec aninda kapandi)")
	}
	return p, nil
}

func verifyStrategy(ctx context.Context, p *brokerProc) verifyResult {
	var metrics []probeMetric
	var rounds []string
	for round := 0; round < verifyRounds; round++ {
		started := time.Now()
		batch := probeTargets(ctx, healthTargets, verifyProbeTimeout, false)
		if batch == nil {
			return verifyResult{canceled: true}
		}
		metrics = append(metrics, batch...)
		c := countHealth(batch)
		rounds = append(rounds, fmt.Sprintf("%d/%d ok %d dns in %dms", c.successes, c.total, c.dnsFailures, time.Since(started).Milliseconds()))
		if !p.alive() || countHealth(metrics).hardFailures > verifyMaxFailures {
			break
		}
		if c.hardFailures == 0 && c.dnsFailures == 0 && c.successes == len(healthTargets) {
			break
		}
	}
	appendEngineLog(fmt.Sprintf("verify %s: %s", p.strategy.ID, strings.Join(rounds, "; ")))

	res := summarizeVerification(metrics)
	if !p.alive() {
		res.state = healthUnhealthy
		return res
	}
	if res.state != healthOK && !internetReachable(ctx) {
		if ctx.Err() != nil {
			return verifyResult{canceled: true}
		}
		res.state = healthOffline
	}
	return res
}

func summarizeVerification(metrics []probeMetric) verifyResult {
	c := countHealth(metrics)
	res := verifyResult{metrics: metrics, ok: c.successes, count: c.successes + c.hardFailures}

	var total int64
	for _, metric := range metrics {
		if isProbeOK(metric.Status) {
			total += metric.Latency
		}
	}
	if c.successes > 0 {
		res.latency = total / int64(c.successes)
	}

	switch {
	case c.hardFailures == 0 && c.dnsFailures == 0 && c.successes == len(healthTargets):
		res.state = healthOK
	case c.hardFailures <= verifyMaxFailures && c.successes >= verifyMinSuccesses && c.total > len(healthTargets):
		res.state = healthOK
	case (c.total-c.dnsFailures)*2 <= c.total:
		res.state = healthInconclusive
	default:
		res.state = healthUnhealthy
	}
	return res
}

func connectStrategies(ctx context.Context, run uint64, phase Phase, order []Strategy, admit func() bool) error {
	connectMu.Lock()
	defer connectMu.Unlock()

	var best *Strategy
	bestOK := -1
	launchFailures := 0
	var lastErr error

	for i, s := range order {
		if ctx.Err() != nil {
			return errScanCanceled
		}
		if i > 0 {
			setPhase(phase, "farkli mod deneniyor")
		}
		emitProgress(ProgressEvent{Run: run, Step: "strategy", State: stepActive, Detail: s.ID, Index: i + 1, Total: len(order)})

		p, err := launchFn(s)
		if err != nil {
			lastErr = err
			launchFailures++
			recordProfileFailure(s.ID, err.Error(), true, false)
			appendEngineLog(fmt.Sprintf("strategy %s failed to launch: %v", s.ID, err))
			emitProgress(ProgressEvent{Run: run, Step: "strategy", State: stepFailed, Detail: s.ID, Index: i + 1, Total: len(order)})
			CleanOldServices()
			if launchFailures >= launchFailureLimit {
				break
			}
			continue
		}
		launchFailures = 0

		res := verifyFn(ctx, p)
		if res.canceled || ctx.Err() != nil {
			p.stop()
			return errScanCanceled
		}

		if res.state != healthUnhealthy {
			state := stepDone
			if res.state != healthOK {
				state = stepSkipped
			}
			emitProgress(ProgressEvent{Run: run, Step: "strategy", State: state, Detail: s.ID, Index: i + 1, Total: len(order), OK: res.ok, Count: res.count, Latency: res.latency})
			reportVerifiedServices(run, res.metrics)
			step(run, "ready", stepDone, s.ID)
			if !adoptBroker(p, res, admit) {
				p.stop()
				return errScanCanceled
			}
			if res.state == healthOK {
				recordProfileProbe(s.ID, res.metrics, healthOK)
				appendEngineLog(fmt.Sprintf("strategy selected=%s handshakes=%d/%d latency=%dms", s.ID, res.ok, res.count, res.latency))
			} else {
				appendEngineLog(fmt.Sprintf("strategy %s adopted without verification (%s): %s", s.ID, healthStateName(res.state), formatHealthSummary(res.metrics)))
			}
			return nil
		}

		recordProfileProbe(s.ID, res.metrics, healthUnhealthy)
		appendEngineLog(fmt.Sprintf("strategy %s rejected: %d/%d handshakes ok; %s", s.ID, res.ok, res.count, formatHealthSummary(res.metrics)))
		emitProgress(ProgressEvent{Run: run, Step: "strategy", State: stepFailed, Detail: s.ID, Index: i + 1, Total: len(order), OK: res.ok, Count: res.count})
		if res.ok > bestOK {
			bestOK = res.ok
			candidate := s
			best = &candidate
		}
		p.stop()
	}

	if best == nil {
		if lastErr != nil {
			return fmt.Errorf("baglanti baslatilamadi: %w", lastErr)
		}
		return fmt.Errorf("baglanti baslatilamadi: uygun mod bulunamadi")
	}
	if ctx.Err() != nil {
		return errScanCanceled
	}

	p, err := launchFn(*best)
	if err != nil {
		return fmt.Errorf("baglanti baslatilamadi: %w", err)
	}
	emitProgress(ProgressEvent{Run: run, Step: "strategy", State: stepSkipped, Detail: best.ID, OK: bestOK})
	step(run, "ready", stepFailed, best.ID)
	if !adoptBroker(p, verifyResult{state: healthInconclusive, partial: true}, admit) {
		p.stop()
		return errScanCanceled
	}
	appendEngineLog(fmt.Sprintf("no strategy passed verification; keeping best partial strategy=%s", best.ID))
	return nil
}

func adoptBroker(p *brokerProc, res verifyResult, admit func() bool) bool {
	healthCtx, cancelHealth := context.WithCancel(context.Background())

	mu.Lock()
	if admit != nil && !admit() {
		mu.Unlock()
		cancelHealth()
		return false
	}
	serviceGeneration++
	generation := serviceGeneration
	winwsProcess = p.cmd
	processDone = p.done
	currentLogFile = p.logFile
	connectionHealthy = res.state == healthOK
	activeStrategy = p.strategy.ID
	healthCheckCancel = cancelHealth
	fallbackUntil = time.Time{}
	if res.partial {
		fallbackUntil = time.Now().Add(fallbackCooldown)
	}
	mu.Unlock()

	recordProfileSelected(p.strategy.ID)

	switch {
	case res.partial:
		setPhase(PhaseRunning, "kismi baglanti")
	case res.state == healthOK:
		setPhase(PhaseRunning, "")
	case res.state == healthOffline:
		setPhase(PhaseRecovering, "internet baglantisi bekleniyor")
	default:
		setPhase(PhaseRunning, "dogrulama suruyor")
	}

	firstCheck := healthInitialDelay
	if res.state != healthOK {
		firstCheck = unverifiedRecheckDelay
	}
	go monitorService(p, generation)
	go healthCheckLoop(healthCtx, p.cmd, p.strategy.ID, firstCheck)
	return true
}

func healthStateName(state healthState) string {
	switch state {
	case healthOK:
		return "ok"
	case healthOffline:
		return "offline"
	case healthUnhealthy:
		return "unhealthy"
	default:
		return "inconclusive"
	}
}

func reportVerifiedServices(run uint64, metrics []probeMetric) {
	for _, check := range groupServiceMetrics(metrics) {
		state := stepDone
		if check.OK == 0 || len(check.Failed) > 0 {
			state = stepFailed
		}
		emitProgress(ProgressEvent{Run: run, Step: "service", State: state, Detail: check.ID, OK: check.OK, Count: check.Count, Latency: check.Latency})
	}
}

func groupServiceMetrics(metrics []probeMetric) []ServiceCheck {
	discord := ServiceCheck{ID: "discord"}
	roblox := ServiceCheck{ID: "roblox"}
	for _, metric := range metrics {
		target := &roblox
		if isDiscordTarget(metric.Target) {
			target = &discord
		}
		addServiceMetric(target, metric)
	}

	var out []ServiceCheck
	for _, check := range []ServiceCheck{discord, roblox} {
		if check.Count > 0 {
			out = append(out, finishServiceCheck(check))
		}
	}
	return out
}

func addServiceMetric(check *ServiceCheck, metric probeMetric) {
	if metric.Status == "dns_failed" {
		return
	}
	check.Count++
	if isProbeOK(metric.Status) {
		check.OK++
		check.Latency += metric.Latency
		return
	}
	for _, failed := range check.Failed {
		if failed == metric.Target {
			return
		}
	}
	check.Failed = append(check.Failed, metric.Target)
}

func finishServiceCheck(check ServiceCheck) ServiceCheck {
	if check.OK > 0 {
		check.Latency /= int64(check.OK)
	} else {
		check.Latency = 0
	}
	if check.Failed == nil {
		check.Failed = []string{}
	}
	return check
}

func checkPaymentServices(ctx context.Context) ServiceCheck {
	check := ServiceCheck{ID: "payments"}
	for _, metric := range probeTargets(ctx, paymentTargets, serviceProbeTime, false) {
		addServiceMetric(&check, metric)
	}
	return finishServiceCheck(check)
}

func reportPaymentServices(run uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), serviceProbeTime+time.Second)
	defer cancel()

	emitProgress(ProgressEvent{Run: run, Step: "service", State: stepActive, Detail: "payments"})
	check := checkPaymentServices(ctx)
	state := stepDone
	if check.Count == 0 || len(check.Failed) > 0 {
		state = stepFailed
	}
	if len(check.Failed) > 0 {
		appendEngineLog("payment services unreachable: " + strings.Join(check.Failed, ", "))
	}
	emitProgress(ProgressEvent{Run: run, Step: "service", State: state, Detail: "payments", OK: check.OK, Count: check.Count, Latency: check.Latency})
}

func CheckServices() []ServiceCheck {
	ctx, cancel := context.WithTimeout(context.Background(), 2*serviceProbeTime)
	defer cancel()

	var wg sync.WaitGroup
	var core []probeMetric
	var payments ServiceCheck
	wg.Add(2)
	go func() {
		defer wg.Done()
		core = probeTargets(ctx, healthTargets, serviceProbeTime, false)
	}()
	go func() {
		defer wg.Done()
		payments = checkPaymentServices(ctx)
	}()
	wg.Wait()

	return append(groupServiceMetrics(core), payments)
}
