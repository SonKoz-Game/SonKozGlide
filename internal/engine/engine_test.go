package engine

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestBuildArgsIncludesQuicStrategy(t *testing.T) {
	args := buildArgs(strategyCatalog[0])
	joined := "\x00" + strings.Join(args, "\x00") + "\x00"

	for _, want := range []string{
		"--wf-tcp=443",
		"--wf-udp=443",
		"--filter-tcp=443",
		"--new",
		"--filter-udp=443",
		"--filter-l7=quic",
		"--dpi-desync-cutoff=n2",
	} {
		if !strings.Contains(joined, "\x00"+want+"\x00") {
			t.Fatalf("buildArgs(%q) missing %q in %v", strategyCatalog[0].ID, want, args)
		}
	}
}

func TestQuicFakePacketsExpireBeforeTheRealServer(t *testing.T) {
	for _, s := range strategyCatalog {
		args := buildArgs(s)
		quic := args[indexOfArg(t, args, "--new"):]

		want := "--dpi-desync-ttl=" + s.QuicTTL
		if !strings.Contains("\x00"+strings.Join(quic, "\x00")+"\x00", "\x00"+want+"\x00") {
			t.Fatalf("strategy %q QUIC fakes must be TTL-limited with %q, got %v", s.ID, want, quic)
		}
	}
}

func TestEveryTCPFakeIsKeptAwayFromTheServer(t *testing.T) {
	for _, s := range strategyCatalog {
		mode := strategyArg(s, "--dpi-desync=")
		if !strings.Contains(mode, "fake") {
			continue
		}
		guarded := strategyArg(s, "--dpi-desync-ttl=") != "" ||
			strategyArg(s, "--dpi-desync-autottl=") != "" ||
			strings.Contains(strategyArg(s, "--dpi-desync-fooling="), "md5sig")
		if !guarded {
			t.Fatalf("strategy %q sends fakes that nothing stops from reaching the real server: %v", s.ID, s.TCP)
		}
	}
}

func TestBadseqStrategiesComeAfterEverySequenceValidFake(t *testing.T) {
	seenBadseq := false
	for _, s := range strategyCatalog {
		badseq := strings.Contains(strategyArg(s, "--dpi-desync-fooling="), "badseq")
		fake := strings.Contains(strategyArg(s, "--dpi-desync="), "fake")
		if badseq {
			seenBadseq = true
			continue
		}
		if seenBadseq && fake {
			t.Fatalf("strategy %q must be tried before the badseq ones: a DPI that drops out-of-window packets ignores badseq fakes entirely", s.ID)
		}
	}
	if strings.Contains(strategyArg(strategyCatalog[0], "--dpi-desync-fooling="), "badseq") {
		t.Fatal("the first strategy must not rely on badseq")
	}
}

func TestStrategyIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range strategyCatalog {
		if s.ID == "" || seen[s.ID] {
			t.Fatalf("duplicate or empty strategy id %q", s.ID)
		}
		seen[s.ID] = true
	}
	for profile, id := range ispPreferredStrategy {
		if !isKnownStrategy(id) {
			t.Fatalf("ISP %q prefers unknown strategy %q", profile, id)
		}
	}
}

func strategyArg(s Strategy, prefix string) string {
	for _, arg := range s.TCP {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func indexOfArg(t *testing.T, args []string, want string) int {
	t.Helper()

	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	t.Fatalf("expected %q in %v", want, args)
	return 0
}

func TestAssessHealthTreatsDNSFailuresAsInconclusive(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "dns_failed", Latency: 3},
		{Target: "gateway.discord.gg", Status: "dns_failed", Latency: 3},
		{Target: "roblox.com", Status: "ok", Latency: 30},
		{Target: "rbxcdn.com", Status: "ok", Latency: 30},
	})

	if state != healthInconclusive {
		t.Fatalf("DNS failures should be inconclusive, got %v (%s)", state, reason)
	}
	if !strings.Contains(reason, "dns resolution failed") {
		t.Fatalf("reason should mention DNS failure, got %q", reason)
	}
}

func TestAssessHealthDetectsTotalDNSFailure(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "dns_failed", Latency: 3},
		{Target: "gateway.discord.gg", Status: "dns_failed", Latency: 3},
		{Target: "cdn.discordapp.com", Status: "dns_failed", Latency: 3},
		{Target: "roblox.com", Status: "dns_failed", Latency: 3},
	})

	if state != healthUnhealthy {
		t.Fatalf("total DNS failure should be unhealthy, got %v (%s)", state, reason)
	}
	if !strings.Contains(reason, "dns resolution failed") {
		t.Fatalf("reason should mention DNS failure, got %q", reason)
	}
}

func TestAssessHealthDetectsDiscordHardFailures(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "timeout", Latency: 3000},
		{Target: "gateway.discord.gg", Status: "timeout", Latency: 3000},
		{Target: "roblox.com", Status: "ok", Latency: 30},
		{Target: "rbxcdn.com", Status: "ok", Latency: 30},
	})

	if state != healthUnhealthy {
		t.Fatalf("Discord hard failures should be unhealthy, got %v (%s)", state, reason)
	}
}

func TestAssessHealthRequiresDiscordMediaOnFullProbeSet(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "ok", Latency: 40},
		{Target: "api.discord.com", Status: "ok", Latency: 42},
		{Target: "gateway.discord.gg", Status: "ok", Latency: 45},
		{Target: "cdn.discordapp.com", Status: "timeout", Latency: 3000},
		{Target: "media.discordapp.net", Status: "timeout", Latency: 3000},
		{Target: "roblox.com", Status: "ok", Latency: 30},
		{Target: "rbxcdn.com", Status: "ok", Latency: 30},
	})

	if state == healthOK {
		t.Fatalf("full probe should not be healthy without Discord media/CDN success (%s)", reason)
	}
}

func TestAssessHealthAcceptsDiscordAndOtherSuccess(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "ok_dns_fallback", Latency: 40},
		{Target: "roblox.com", Status: "ok", Latency: 30},
	})

	if state != healthOK {
		t.Fatalf("expected healthy state, got %v (%s)", state, reason)
	}
}

func TestAssessHealthAcceptsCoreAndMediaDiscordSuccess(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "ok", Latency: 40},
		{Target: "gateway.discord.gg", Status: "ok", Latency: 45},
		{Target: "cdn.discordapp.com", Status: "ok_dns_fallback", Latency: 70},
		{Target: "roblox.com", Status: "ok", Latency: 30},
		{Target: "rbxcdn.com", Status: "ok", Latency: 30},
	})

	if state != healthOK {
		t.Fatalf("expected healthy state with Discord core and media success, got %v (%s)", state, reason)
	}
}

func TestAssessHealthRejectsAHalfBlockedLine(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "media.discordapp.net", Status: "ok", Latency: 217},
		{Target: "discordapp.com", Status: "reset", Latency: 264},
		{Target: "tr.rbxcdn.com", Status: "ok", Latency: 269},
		{Target: "gateway.discord.gg", Status: "reset", Latency: 300},
		{Target: "roblox.com", Status: "ok", Latency: 301},
		{Target: "apis.roblox.com", Status: "ok", Latency: 323},
		{Target: "cdn.discordapp.com", Status: "reset", Latency: 323},
		{Target: "discord.com", Status: "reset", Latency: 368},
	})

	if state != healthUnhealthy {
		t.Fatalf("a line where half the handshakes are reset is not healthy, got %v (%s)", state, reason)
	}
}

func TestAssessHealthToleratesOneStrayFailure(t *testing.T) {
	metrics := make([]probeMetric, 0, len(healthTargets))
	for i, target := range healthTargets {
		status := "ok"
		if i == len(healthTargets)-1 {
			status = "timeout"
		}
		metrics = append(metrics, probeMetric{Target: target, Status: status, Latency: 90})
	}

	if state, reason := assessHealth(metrics); state != healthOK {
		t.Fatalf("one slow target out of %d must not restart a working bypass, got %v (%s)", len(metrics), state, reason)
	}
}

func verificationMetrics(okCount int, total int) []probeMetric {
	metrics := make([]probeMetric, 0, total)
	for i := 0; i < total; i++ {
		status := "reset"
		if i < okCount {
			status = "ok"
		}
		metrics = append(metrics, probeMetric{Target: healthTargets[i%len(healthTargets)], Status: status, Latency: 80})
	}
	return metrics
}

func TestVerificationSeparatesWorkingFromHalfBlockedStrategies(t *testing.T) {
	cases := []struct {
		ok    int
		total int
		want  healthState
	}{
		{8, 8, healthOK},
		{7, 8, healthUnhealthy},
		{16, 16, healthOK},
		{15, 16, healthOK},
		{14, 16, healthUnhealthy},
		{7, 16, healthUnhealthy},
		{5, 10, healthUnhealthy},
	}
	for _, c := range cases {
		if got := summarizeVerification(verificationMetrics(c.ok, c.total)).state; got != c.want {
			t.Fatalf("%d/%d handshakes: expected %v, got %v", c.ok, c.total, c.want, got)
		}
	}
}

func TestClassifyProbeErrorRecognisesInjectedResets(t *testing.T) {
	err := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("wsarecv", windows.WSAECONNRESET)}
	if got := classifyProbeError(err); got != "reset" {
		t.Fatalf("a DPI-injected RST must be classified as reset, got %q", got)
	}
}

func TestStrategyOrderPrefersLearnedFastStableStrategy(t *testing.T) {
	useAdaptiveStateFile(t)

	now := time.Now()
	state := defaultAdaptiveState()
	state.ProfileStats["md5-disorder"] = adaptiveProfileStats{Samples: 5, SuccessEWMA: 1, LatencyEWMA: 520, StabilityEWMA: 1, LastSuccessAt: now}
	state.ProfileStats["ttl-fake"] = adaptiveProfileStats{Samples: 5, SuccessEWMA: 1, LatencyEWMA: 85, StabilityEWMA: 1, LastSuccessAt: now}
	writeAdaptiveStateForTest(state)

	if got := strategyOrder("", "")[0].ID; got != "ttl-fake" {
		t.Fatalf("expected the fastest learned strategy first, got %q", got)
	}
}

func TestStrategyOrderPushesAMeasuredFailureBehindUntriedStrategies(t *testing.T) {
	useAdaptiveStateFile(t)

	recordProfileProbe(strategyCatalog[0].ID, verificationMetrics(7, 16), healthUnhealthy)

	order := strategyOrder("", "")
	if order[0].ID != strategyCatalog[1].ID || order[len(order)-1].ID != strategyCatalog[0].ID {
		t.Fatalf("a strategy that just failed verification must fall behind every untried one, got first=%q last=%q", order[0].ID, order[len(order)-1].ID)
	}
}

func TestStrategyOrderHonoursPreferenceAndDemotion(t *testing.T) {
	useAdaptiveStateFile(t)

	order := strategyOrder("md5-split", "")
	if order[0].ID != "md5-split" || len(order) != len(strategyCatalog) {
		t.Fatalf("preferred strategy must lead a complete order, got %q (%d)", order[0].ID, len(order))
	}

	order = strategyOrder("md5-disorder", "md5-disorder")
	if order[len(order)-1].ID != "md5-disorder" || order[0].ID == "md5-disorder" {
		t.Fatalf("a demoted strategy must be tried last, got first=%q last=%q", order[0].ID, order[len(order)-1].ID)
	}
}

func TestRecordProfileProbeLearnsHealthMetrics(t *testing.T) {
	useAdaptiveStateFile(t)

	recordProfileProbe("md5-fake", []probeMetric{
		{Target: "discord.com", Status: "ok", Latency: 100},
		{Target: "roblox.com", Status: "ok", Latency: 80},
	}, healthOK)

	state := readAdaptiveStateForTest()
	stats := state.ProfileStats["md5-fake"]
	if stats.Samples != 1 {
		t.Fatalf("expected one learned sample, got %d", stats.Samples)
	}
	if stats.SuccessEWMA != 1 || stats.StabilityEWMA != 1 {
		t.Fatalf("expected healthy EWMA values, got success=%v stability=%v", stats.SuccessEWMA, stats.StabilityEWMA)
	}
	if stats.LatencyEWMA != 90 {
		t.Fatalf("expected average latency 90ms, got %v", stats.LatencyEWMA)
	}
	if stats.LastSuccessAt.IsZero() {
		t.Fatal("expected LastSuccessAt to be set")
	}
}

func TestRecordProfileProbeLearnsInconclusiveMetrics(t *testing.T) {
	useAdaptiveStateFile(t)

	recordProfileProbe("md5-fake", []probeMetric{
		{Target: "discord.com", Status: "dns_failed", Latency: 100},
		{Target: "cdn.discordapp.com", Status: "ok_dns_fallback", Latency: 120},
		{Target: "roblox.com", Status: "ok", Latency: 80},
	}, healthInconclusive)

	state := readAdaptiveStateForTest()
	stats := state.ProfileStats["md5-fake"]
	if stats.Samples != 1 {
		t.Fatalf("expected one learned sample, got %d", stats.Samples)
	}
	if stats.SuccessEWMA <= 0 || stats.SuccessEWMA >= 1 {
		t.Fatalf("expected partial success EWMA, got %v", stats.SuccessEWMA)
	}
	if stats.StabilityEWMA != 0.35 {
		t.Fatalf("expected inconclusive stability sample, got %v", stats.StabilityEWMA)
	}
}

func TestAdaptiveStateFromTheOldProfileEraIsDiscarded(t *testing.T) {
	useAdaptiveStateFile(t)

	legacy := `{"version":1,"lastProfile":"auto","profileStats":{"auto":{"samples":2760,"successEwma":0.87}}}`
	if err := os.WriteFile(adaptiveStatePathOverride, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	state := readAdaptiveStateForTest()
	if state.Version != adaptiveStateVer || len(state.ProfileStats) != 0 || state.LastProfile != "" {
		t.Fatalf("stats learned for the old badseq profiles must not steer the new catalog: %+v", state)
	}
}

type fakeBroker struct {
	launched []string
	procs    []*brokerProc
}

func installFakeBroker(t *testing.T, verdicts map[string]verifyResult) *fakeBroker {
	t.Helper()
	useAdaptiveStateFile(t)

	fake := &fakeBroker{}
	oldLaunch, oldVerify := launchFn, verifyFn
	launchFn = func(s Strategy) (*brokerProc, error) {
		fake.launched = append(fake.launched, s.ID)
		p := &brokerProc{cmd: &exec.Cmd{}, done: make(chan struct{}), started: time.Now(), strategy: s}
		fake.procs = append(fake.procs, p)
		return p, nil
	}
	verifyFn = func(ctx context.Context, p *brokerProc) verifyResult {
		if res, ok := verdicts[p.strategy.ID]; ok {
			return res
		}
		return summarizeVerification(verificationMetrics(7, 16))
	}
	t.Cleanup(func() {
		launchFn, verifyFn = oldLaunch, oldVerify
		mu.Lock()
		isIntentionalStop = true
		autoRestart = false
		winwsProcess = nil
		processDone = nil
		activeStrategy = ""
		connectionHealthy = false
		fallbackUntil = time.Time{}
		if healthCheckCancel != nil {
			healthCheckCancel()
			healthCheckCancel = nil
		}
		mu.Unlock()
		for _, p := range fake.procs {
			close(p.done)
		}
		setPhase(PhaseIdle, "")
	})
	return fake
}

func TestConnectStrategiesSkipsAHalfBlockedStrategy(t *testing.T) {
	order := strategyOrder("", "")
	working := order[2].ID
	fake := installFakeBroker(t, map[string]verifyResult{
		working: summarizeVerification(verificationMetrics(16, 16)),
	})

	if err := connectStrategies(context.Background(), 0, PhaseStarting, order, func() bool { return false }); err != errScanCanceled {
		t.Fatalf("a refused admission must cancel, got %v", err)
	}
	fake.launched = nil

	if err := connectStrategies(context.Background(), 0, PhaseStarting, order, nil); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if got := strings.Join(fake.launched, ","); got != strings.Join([]string{order[0].ID, order[1].ID, working}, ",") {
		t.Fatalf("expected the scan to stop at the first verified strategy, launched %s", got)
	}
	if s := State(); s.Profile != working || s.Phase != PhaseRunning || !connectionHealthyForTest() {
		t.Fatalf("expected %q running and verified, got %+v", working, s)
	}
}

func TestConnectStrategiesKeepsTheBestPartialStrategyWhenNothingPasses(t *testing.T) {
	order := strategyOrder("", "")
	best := order[3].ID
	installFakeBroker(t, map[string]verifyResult{
		best: summarizeVerification(verificationMetrics(12, 16)),
	})

	if err := connectStrategies(context.Background(), 0, PhaseStarting, order, nil); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	s := State()
	if s.Profile != best || s.Detail != "kismi baglanti" {
		t.Fatalf("expected the best partial strategy %q to be kept, got %+v", best, s)
	}
	if !inFallbackCooldown() {
		t.Fatal("a partial connection must not trigger a rescan every minute")
	}
}

func TestConnectStrategiesStopsWhenCancelled(t *testing.T) {
	fake := installFakeBroker(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := connectStrategies(ctx, 0, PhaseStarting, strategyOrder("", ""), nil); err != errScanCanceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if len(fake.launched) != 0 {
		t.Fatalf("a cancelled scan must not launch anything, launched %v", fake.launched)
	}
}

func connectionHealthyForTest() bool {
	mu.Lock()
	defer mu.Unlock()
	return connectionHealthy
}

func useAdaptiveStateFile(t *testing.T) {
	t.Helper()

	oldPath := adaptiveStatePathOverride
	adaptiveStatePathOverride = filepath.Join(t.TempDir(), "adaptive_profiles.json")
	t.Cleanup(func() {
		adaptiveStatePathOverride = oldPath
	})
}

func writeAdaptiveStateForTest(state adaptiveState) {
	adaptiveMu.Lock()
	defer adaptiveMu.Unlock()
	saveAdaptiveStateLocked(state)
}

func readAdaptiveStateForTest() adaptiveState {
	adaptiveMu.Lock()
	defer adaptiveMu.Unlock()
	return loadAdaptiveStateLocked()
}

func useTempTargetDir(t *testing.T) {
	t.Helper()
	t.Setenv("PROGRAMDATA", t.TempDir())
}
