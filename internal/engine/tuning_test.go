package engine

import (
	"testing"
)

func stubPingProbe(t *testing.T, maxPayload int, maxPlainPayload int) *[]int {
	t.Helper()

	var asked []int
	oldDF, oldPlain := pingProbe, plainPingProbe
	pingProbe = func(target string, payload int) bool {
		asked = append(asked, payload)
		return payload <= maxPayload
	}
	plainPingProbe = func(target string, payload int) bool {
		return payload <= maxPlainPayload
	}
	t.Cleanup(func() { pingProbe, plainPingProbe = oldDF, oldPlain })
	return &asked
}

func TestProbePathMTUDetectsPPPoEPath(t *testing.T) {
	stubPingProbe(t, 1492-mtuOverhead, 9000)

	mtu, ok := probePathMTU()
	if !ok {
		t.Fatal("expected a conclusive measurement")
	}
	if mtu != 1492 {
		t.Fatalf("expected the PPPoE path MTU 1492, got %d", mtu)
	}
}

func TestProbePathMTUReportsFullPath(t *testing.T) {
	asked := stubPingProbe(t, 9000, 9000)

	mtu, ok := probePathMTU()
	if !ok || mtu != 1500 {
		t.Fatalf("expected 1500, got %d (ok=%v)", mtu, ok)
	}
	if len(*asked) > 2 {
		t.Fatalf("a healthy path must cost at most a reachability probe plus one test, got %d", len(*asked))
	}
}

func TestProbePathMTUGivesUpWhenICMPIsBlocked(t *testing.T) {
	stubPingProbe(t, 0, 0)

	if _, ok := probePathMTU(); ok {
		t.Fatal("a blocked ICMP path must be inconclusive, never a reason to lower the MTU")
	}
}

func TestProbePathMTUIgnoresRateLimitedICMP(t *testing.T) {
	stubPingProbe(t, 1460-mtuOverhead, 1460-mtuOverhead)

	if mtu, ok := probePathMTU(); ok {
		t.Fatalf("large packets that fail with and without DF prove nothing about the path MTU, got %d", mtu)
	}
}

func TestProbePathMTUNeverLowersBelowTheAutoFloor(t *testing.T) {
	stubPingProbe(t, mtuFloor-mtuOverhead, 9000)

	if mtu, ok := probePathMTU(); ok {
		t.Fatalf("an implausibly small path must be left alone, got %d", mtu)
	}
}

func TestSetInterfaceMTURejectsUnsafeValues(t *testing.T) {
	for _, mtu := range []int{0, -1, 576, 1279} {
		if setInterfaceMTU(12, mtu) {
			t.Fatalf("MTU %d must be rejected before it reaches netsh", mtu)
		}
	}
	if setInterfaceMTU(0, 1492) {
		t.Fatal("an invalid interface index must be rejected")
	}
}

func TestAutoTuningLevelRejectsInjection(t *testing.T) {
	bad := []string{"", "normal & shutdown /s", "normal;calc", "$(evil)", "highly restricted"}
	for _, value := range bad {
		if _, ok := normalizeAutoTuningLevel(value); ok {
			t.Fatalf("expected %q to be rejected", value)
		}
		if setAutoTuningLevel(value) {
			t.Fatalf("expected %q to never reach netsh", value)
		}
	}

	for raw, want := range map[string]string{
		"Normal":           "normal",
		" disabled\r\n":    "disabled",
		"HighlyRestricted": "highlyrestricted",
		"Restricted":       "restricted",
	} {
		got, ok := normalizeAutoTuningLevel(raw)
		if !ok || got != want {
			t.Fatalf("normalizeAutoTuningLevel(%q) = %q,%v; want %q", raw, got, ok, want)
		}
	}
}

func stubAutoTuning(t *testing.T, current string, readOK bool, setOK bool) *[]string {
	t.Helper()

	var applied []string
	oldReader, oldSetter := autoTuningReader, autoTuningSetter
	autoTuningReader = func() (string, bool) { return current, readOK }
	autoTuningSetter = func(level string) bool {
		if setOK {
			applied = append(applied, level)
		}
		return setOK
	}
	t.Cleanup(func() { autoTuningReader, autoTuningSetter = oldReader, oldSetter })
	return &applied
}

func TestApplyAutoTuningLeavesHealthySystemsUntouched(t *testing.T) {
	applied := stubAutoTuning(t, "normal", true, true)
	backup := tuningBackup{}

	finding := applyAutoTuning(&backup)

	if finding.Status != tuningStatusOK {
		t.Fatalf("an already-normal system must report ok, got %q", finding.Status)
	}
	if len(*applied) != 0 {
		t.Fatalf("a healthy system must not be reconfigured, got %v", *applied)
	}
	if backup.AutoTuning != "" {
		t.Fatalf("nothing must be recorded when nothing changed, got %q", backup.AutoTuning)
	}
}

func TestApplyAutoTuningFixesARestrictedSystem(t *testing.T) {
	applied := stubAutoTuning(t, "highlyrestricted", true, true)
	backup := tuningBackup{}

	finding := applyAutoTuning(&backup)

	if finding.Status != tuningStatusFixed {
		t.Fatalf("expected a fix, got %q (%s)", finding.Status, finding.Detail)
	}
	if len(*applied) != 1 || (*applied)[0] != preferredAutoTuning {
		t.Fatalf("expected exactly one switch to normal, got %v", *applied)
	}
	if backup.AutoTuning != "highlyrestricted" {
		t.Fatalf("the original level must be recorded for restore, got %q", backup.AutoTuning)
	}
}

func TestApplyAutoTuningKeepsTheTrueOriginalWhenAReapplyFails(t *testing.T) {
	stubAutoTuning(t, "restricted", true, false)
	backup := tuningBackup{AutoTuning: "disabled"}

	finding := applyAutoTuning(&backup)

	if finding.Status != tuningStatusSkipped {
		t.Fatalf("a failed apply must not claim success, got %q", finding.Status)
	}
	if backup.AutoTuning != "disabled" {
		t.Fatalf("a failed re-apply must not discard the original level, got %q", backup.AutoTuning)
	}
}

func TestApplyAutoTuningRecordsNothingWhenTheFirstAttemptFails(t *testing.T) {
	stubAutoTuning(t, "restricted", true, false)
	backup := tuningBackup{}

	applyAutoTuning(&backup)

	if backup.AutoTuning != "" {
		t.Fatalf("nothing must be recorded for a change that never happened, got %q", backup.AutoTuning)
	}
}

func TestApplyAutoTuningSkipsWhenTheLevelCannotBeRead(t *testing.T) {
	applied := stubAutoTuning(t, "", false, true)
	backup := tuningBackup{}

	finding := applyAutoTuning(&backup)

	if finding.Status != tuningStatusSkipped {
		t.Fatalf("an unreadable system must be left alone, got %q", finding.Status)
	}
	if len(*applied) != 0 {
		t.Fatalf("nothing must be changed blindly, got %v", *applied)
	}
}

func TestTuningInterfacesOnlyTouchIPv4GatewayAdapters(t *testing.T) {
	got := tuningInterfacesFrom([]netInterface{
		{Index: 12, Alias: "Ethernet", MTU: 1500, HasIPv4Gateway: true},
		{Index: 9, Alias: "IPv6 tunnel", MTU: 1280, HasIPv4Gateway: false},
		{Index: 14, Alias: "Wi-Fi", MTU: 1492, HasIPv4Gateway: true},
	})

	if len(got) != 2 || got[0].Index != 12 || got[1].Index != 14 || got[1].MTU != 1492 {
		t.Fatalf("only IPv4-routed adapters may have their MTU tuned, got %+v", got)
	}
	if empty := tuningInterfacesFrom(nil); len(empty) != 0 {
		t.Fatalf("no adapters must be benign, got %+v", empty)
	}
}

func TestEnsureNetworkTuningRunsOncePerSession(t *testing.T) {
	useTempTargetDir(t)
	stubAutoTuning(t, "normal", true, true)
	stubPingProbe(t, 1500-mtuOverhead, 9000)

	listed := 0
	oldLister := tuningIfaceLister
	tuningIfaceLister = func() ([]tuningInterfaceBackup, error) {
		listed++
		return []tuningInterfaceBackup{{Index: 12, Alias: "Ethernet", MTU: 1500}}, nil
	}
	t.Cleanup(func() {
		tuningIfaceLister = oldLister
		tuningMu.Lock()
		tuningWanted, tuningDone = false, false
		tuningMu.Unlock()
	})

	applyNetworkTuning()
	applyNetworkTuning()
	if listed != 1 {
		t.Fatalf("a restart must not re-probe an already tuned session, probed %d times", listed)
	}

	setTuningWanted(false)
	restoreTuningIfUnwanted()
	applyNetworkTuning()
	if listed != 2 {
		t.Fatalf("a real stop/start must tune again, probed %d times", listed)
	}
}

func TestUpsertInterfaceBackupKeepsFirstOriginal(t *testing.T) {
	saved := upsertInterfaceBackup(nil, tuningInterfaceBackup{Index: 12, MTU: 1500})
	saved = upsertInterfaceBackup(saved, tuningInterfaceBackup{Index: 12, MTU: 1492})

	if len(saved) != 1 || saved[0].MTU != 1500 {
		t.Fatalf("re-applying must not overwrite the true original: %+v", saved)
	}
}

func applyNetworkTuning() {
	setTuningWanted(true)
	ensureNetworkTuning()
}
