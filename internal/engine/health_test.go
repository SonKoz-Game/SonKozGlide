package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SonKoz-Game/SonKozGlide/internal/router"
)

func TestHealthTargetsAreAllDesyncedByTheHostlist(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "rules", "rules.yaml"))
	if err != nil {
		t.Fatalf("read rules: %v", err)
	}
	rs, err := router.Compile(data)
	if err != nil {
		t.Fatalf("compile rules: %v", err)
	}
	hostlist := rs.HostlistProjection()

	for _, target := range healthTargets {
		covered := false
		for _, entry := range hostlist {
			if target == entry || strings.HasSuffix(target, "."+entry) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("health target %q is not in the hostlist, so its probe would not measure the bypass", target)
		}
	}
}

func TestHealthTargetsAvoidNamesWithoutAddressRecords(t *testing.T) {
	for _, target := range healthTargets {
		switch target {
		case "api.discord.com", "rbxcdn.com":
			t.Fatalf("%q has no A record; a probe that can never resolve pins every verdict to inconclusive", target)
		}
	}
}

func TestAssessHealthStillDetectsAnOutageWhenSomeNamesDoNotResolve(t *testing.T) {
	state, reason := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "timeout", Latency: 5000},
		{Target: "api.discord.com", Status: "dns_failed", Latency: 5000},
		{Target: "gateway.discord.gg", Status: "timeout", Latency: 5000},
		{Target: "discordapp.com", Status: "timeout", Latency: 5000},
		{Target: "cdn.discordapp.com", Status: "timeout", Latency: 5000},
		{Target: "media.discordapp.net", Status: "ok", Latency: 120},
		{Target: "roblox.com", Status: "failed", Latency: 3370},
		{Target: "apis.roblox.com", Status: "timeout", Latency: 5000},
		{Target: "rbxcdn.com", Status: "dns_failed", Latency: 19},
	})

	if state != healthUnhealthy {
		t.Fatalf("a Discord outage must trigger recovery even when two names never resolve, got %v (%s)", state, reason)
	}
}

func TestAssessHealthWaitsWhenMostNamesDoNotResolve(t *testing.T) {
	state, _ := assessHealth([]probeMetric{
		{Target: "discord.com", Status: "dns_failed"},
		{Target: "gateway.discord.gg", Status: "dns_failed"},
		{Target: "discordapp.com", Status: "dns_failed"},
		{Target: "cdn.discordapp.com", Status: "dns_failed"},
		{Target: "media.discordapp.net", Status: "dns_failed"},
		{Target: "roblox.com", Status: "timeout"},
		{Target: "apis.roblox.com", Status: "ok"},
		{Target: "tr.rbxcdn.com", Status: "ok"},
	})

	if state != healthInconclusive {
		t.Fatalf("a DNS outage is not the bypass's fault and must not restart it, got %v", state)
	}
}

func TestProbeTargetListStopsAsSoonAsHealthIsConfirmed(t *testing.T) {
	old := probeFn
	t.Cleanup(func() { probeFn = old })

	slowest := healthTargets[len(healthTargets)-1]
	probeFn = func(ctx context.Context, target string, timeout time.Duration) probeMetric {
		if target != slowest {
			return probeMetric{Target: target, Status: "ok", Latency: 40}
		}
		<-ctx.Done()
		return probeMetric{Target: target, Status: "failed"}
	}

	start := time.Now()
	metrics := probeTargetList(context.Background(), healthTargets, 5*time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a confirmed-healthy probe must not wait for the slowest target, took %s", elapsed)
	}
	if state, reason := assessHealth(metrics); state != healthOK {
		t.Fatalf("the early result must still assess as healthy, got %v (%s)", state, reason)
	}
}

func TestProbeTargetListWaitsForEveryTargetWhenUnhealthy(t *testing.T) {
	old := probeFn
	t.Cleanup(func() { probeFn = old })

	probeFn = func(ctx context.Context, target string, timeout time.Duration) probeMetric {
		return probeMetric{Target: target, Status: "timeout"}
	}

	if metrics := probeTargetList(context.Background(), healthTargets, time.Second); len(metrics) != len(healthTargets) {
		t.Fatalf("a failing probe must report every target, got %d of %d", len(metrics), len(healthTargets))
	}
}

func TestProbeTargetListReturnsNothingWhenCancelled(t *testing.T) {
	old := probeFn
	t.Cleanup(func() { probeFn = old })

	ctx, cancel := context.WithCancel(context.Background())
	probeFn = func(probeCtx context.Context, target string, timeout time.Duration) probeMetric {
		cancel()
		<-probeCtx.Done()
		return probeMetric{Target: target, Status: "failed"}
	}

	if metrics := probeTargetList(ctx, healthTargets, time.Second); metrics != nil {
		t.Fatalf("a stopped service must not record a probe, got %v", metrics)
	}
}

func TestParseDoHAnswerKeepsOnlyPublicIPv4Records(t *testing.T) {
	var answer dohResponse
	answer.Answer = append(answer.Answer,
		struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}{Type: 5, Data: "edge.example."},
		struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}{Type: 1, Data: "162.159.133.234"},
		struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}{Type: 1, Data: "162.159.133.234"},
		struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}{Type: 1, Data: "10.0.0.1"},
		struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}{Type: 1, Data: "0.0.0.0"},
	)

	ips := parseDoHAnswer(answer)
	if len(ips) != 1 || ips[0].String() != "162.159.133.234" {
		t.Fatalf("expected one public address, got %v", ips)
	}

	answer.Status = 3
	if ips := parseDoHAnswer(answer); ips != nil {
		t.Fatalf("an NXDOMAIN answer must yield nothing, got %v", ips)
	}
}

func TestTerminateProcessesByNameStopsOnlyMatchingImages(t *testing.T) {
	system := os.Getenv("SystemRoot")
	if system == "" {
		t.Skip("SystemRoot not set")
	}
	source, err := os.ReadFile(filepath.Join(system, "System32", "PING.EXE"))
	if err != nil {
		t.Skipf("ping.exe unavailable: %v", err)
	}

	name := "glide_kill_test.exe"
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, source, 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(path, "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	if !waitForProcessStable(cmd, 200*time.Millisecond) {
		t.Fatal("the test process should still be running")
	}

	start := time.Now()
	if !terminateProcessesByName([]string{"GLIDE_KILL_TEST.EXE"}, 2*time.Second) {
		t.Fatal("terminating a process we own must succeed")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the matching process is still running")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("native termination should be near-instant, took %s", elapsed)
	}

	if !terminateProcessesByName([]string{"glide_no_such_process.exe"}, time.Second) {
		t.Fatal("nothing to terminate must count as success")
	}
}

func TestListGatewayInterfacesReadsThisMachine(t *testing.T) {
	start := time.Now()
	ifaces, err := listGatewayInterfaces()
	if err != nil {
		t.Fatalf("GetAdaptersAddresses failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("native interface listing must be fast, took %s", elapsed)
	}
	for _, iface := range ifaces {
		if iface.Index <= 0 || !strings.HasPrefix(iface.GUID, "{") || iface.Alias == "" {
			t.Fatalf("incomplete interface record: %+v", iface)
		}
		if iface.HasIPv4Gateway && (iface.MTU < 576 || iface.MTU > 65535) {
			t.Fatalf("implausible IPv4 MTU: %+v", iface)
		}
		t.Logf("%d %q %s mtu=%d v4gw=%v dns=%v", iface.Index, iface.Alias, iface.GUID, iface.MTU, iface.HasIPv4Gateway, iface.DNSServers)
	}
}
