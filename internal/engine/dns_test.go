package engine

import (
	"testing"
	"time"
)

func TestPSServerListQuotesAndFilters(t *testing.T) {
	got := psServerList("1.1.1.1, 8.8.8.8 ,,not-an-ip;rm,2001:4860:4860::8888")
	want := "'1.1.1.1','8.8.8.8','2001:4860:4860::8888'"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestIsPlausibleIPRejectsInjection(t *testing.T) {
	bad := []string{"", "1.1.1.1; Remove-Item", "$(evil)", "8.8.8.8 ", "host.name"}
	for _, value := range bad {
		if isPlausibleIP(value) {
			t.Fatalf("expected %q to be rejected", value)
		}
	}

	good := []string{"1.1.1.1", "8.8.4.4", "2001:4860:4860::8888", "fe80::1"}
	for _, value := range good {
		if !isPlausibleIP(value) {
			t.Fatalf("expected %q to be accepted", value)
		}
	}
}

func TestDNSServerCSV(t *testing.T) {
	if got := dnsServerCSV(false); got != "1.1.1.1,8.8.8.8" {
		t.Fatalf("unexpected IPv4 server CSV: %q", got)
	}

	want := "1.1.1.1,8.8.8.8,2606:4700:4700::1111,2001:4860:4860::8888"
	if got := dnsServerCSV(true); got != want {
		t.Fatalf("unexpected dual-stack server CSV: %q", got)
	}
}

func TestEncryptedDNSKeepsBothProvidersPerFamily(t *testing.T) {
	for _, servers := range [][]dohServer{encryptedDNSServers, encryptedDNSServersV6} {
		if len(servers) != 2 {
			t.Fatalf("each family needs exactly two resolvers so a dead one fails over fast, got %d", len(servers))
		}
		if servers[0].Template == servers[1].Template {
			t.Fatalf("both resolvers point at the same provider: %+v", servers)
		}
	}
}

func TestOriginalServerCSVSkipsDHCPFamilies(t *testing.T) {
	iface := dnsInterfaceBackup{Mode: "dhcp", Servers: "", Mode6: "static", Servers6: "fd00::1"}
	if csv := originalServerCSV(iface); csv != "fd00::1" {
		t.Fatalf("expected only the static family, got %q", csv)
	}

	iface = dnsInterfaceBackup{Mode: "dhcp", Mode6: "dhcp"}
	if csv := originalServerCSV(iface); csv != "" {
		t.Fatalf("expected a pure DHCP adapter to restore via reset only, got %q", csv)
	}
}

func TestBackupFromInterfaceKeepsStaticServersPerFamily(t *testing.T) {
	iface := netInterface{
		Index:      6,
		Alias:      "Ethernet",
		GUID:       "{abc}",
		DNSServers: []string{"192.168.1.1", "fd00::1"},
	}

	got := backupFromInterface(iface, "1.1.1.1,9.9.9.9", "")
	if got.Mode != "static" || got.Servers != "1.1.1.1,9.9.9.9" {
		t.Fatalf("a static IPv4 resolver list must be restored verbatim, got %+v", got)
	}
	if got.Mode6 != "dhcp" || got.Servers6 != "fd00::1" {
		t.Fatalf("an unset IPv6 family must restore via reset, got %+v", got)
	}
	if csv := originalServerCSV(got); csv != "1.1.1.1,9.9.9.9" {
		t.Fatalf("only the static family may be re-applied on restore, got %q", csv)
	}

	dhcp := backupFromInterface(iface, "", "")
	if dhcp.Mode != "dhcp" || dhcp.Servers != "192.168.1.1" || originalServerCSV(dhcp) != "" {
		t.Fatalf("a DHCP adapter must restore via reset only, got %+v", dhcp)
	}
}

func TestNormalizeServerListAcceptsRegistrySeparators(t *testing.T) {
	for raw, want := range map[string]string{
		"1.1.1.1,8.8.8.8": "1.1.1.1,8.8.8.8",
		"1.1.1.1 8.8.8.8": "1.1.1.1,8.8.8.8",
		" 1.1.1.1 ,, ":    "1.1.1.1",
		"":                "",
	} {
		if got := normalizeServerList(raw); got != want {
			t.Fatalf("normalizeServerList(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseIndexResultsSeparatesSuccessAndFailure(t *testing.T) {
	ok, failures := parseIndexResults("ok 6\r\nfail 12 Access denied\r\n\r\nnoise\r\nok 14\r\n")
	if !ok[6] || !ok[14] || ok[12] || len(ok) != 2 {
		t.Fatalf("unexpected successes: %v", ok)
	}
	if len(failures) != 1 || failures[0] != "12 Access denied" {
		t.Fatalf("unexpected failures: %v", failures)
	}
}

type dnsStub struct {
	ifaces   []netInterface
	applied  [][]int
	restored int
	verifyOK bool
}

func installDNSStub(t *testing.T, stub *dnsStub) {
	t.Helper()
	useTempTargetDir(t)

	oldLister, oldApplier, oldRestorer := dnsInterfaceLister, dnsServerApplier, dnsServerRestorer
	oldTemplates, oldFlush, oldVerifier := dnsTemplateSetter, dnsCacheFlusher, dnsVerifier

	dnsInterfaceLister = func() ([]netInterface, error) { return stub.ifaces, nil }
	dnsServerApplier = func(ifaces []netInterface) map[int]bool {
		indexes := make([]int, 0, len(ifaces))
		ok := make(map[int]bool)
		for _, iface := range ifaces {
			indexes = append(indexes, iface.Index)
			ok[iface.Index] = true
		}
		stub.applied = append(stub.applied, indexes)
		return ok
	}
	dnsServerRestorer = func([]dnsInterfaceBackup) { stub.restored++ }
	dnsTemplateSetter = func(bool) {}
	dnsCacheFlusher = func() {}
	dnsVerifier = func() (time.Duration, bool) { return 20 * time.Millisecond, stub.verifyOK }

	t.Cleanup(func() {
		dnsInterfaceLister, dnsServerApplier, dnsServerRestorer = oldLister, oldApplier, oldRestorer
		dnsTemplateSetter, dnsCacheFlusher, dnsVerifier = oldTemplates, oldFlush, oldVerifier

		dnsMu.Lock()
		dnsWanted, dnsTemplatesOn, dnsAttempted, dnsAttemptSig = false, false, false, ""
		dnsAppliedIfaces = map[int]bool{}
		dnsMu.Unlock()
		setDNSFinding(nil)
	})
}

func waitForDNSRefresh(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for dnsRefreshBusy.Load() {
		if time.Now().After(deadline) {
			t.Fatal("dns refresh did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEncryptedDNSIsAppliedOncePerInterface(t *testing.T) {
	stub := &dnsStub{ifaces: []netInterface{{Index: 6, Alias: "Ethernet"}}, verifyOK: true}
	installDNSStub(t, stub)

	applyEncryptedDNS()
	applyEncryptedDNS()

	if len(stub.applied) != 1 {
		t.Fatalf("a restart must not re-apply DNS to an interface that already has it, applied %v", stub.applied)
	}
	backup, err := loadDNSBackup()
	if err != nil || !backup.Applied || !backup.has(6) {
		t.Fatalf("the original settings must be backed up before anything is changed, got %+v (%v)", backup, err)
	}
}

func TestEncryptedDNSFollowsANewNetworkInterface(t *testing.T) {
	stub := &dnsStub{verifyOK: true}
	installDNSStub(t, stub)

	applyEncryptedDNS()
	if len(stub.applied) != 0 {
		t.Fatalf("nothing can be applied while offline, applied %v", stub.applied)
	}

	stub.ifaces = []netInterface{{Index: 12, Alias: "Wi-Fi"}}
	refreshEncryptedDNSIfNetworkChanged()
	waitForDNSRefresh(t)

	if len(stub.applied) != 1 || stub.applied[0][0] != 12 {
		t.Fatalf("an interface that comes up later must get encrypted DNS too, applied %v", stub.applied)
	}

	refreshEncryptedDNSIfNetworkChanged()
	waitForDNSRefresh(t)
	if len(stub.applied) != 1 {
		t.Fatalf("an unchanged network must not trigger another apply, applied %v", stub.applied)
	}
}

func TestEncryptedDNSRevertsWhenItDoesNotResolve(t *testing.T) {
	stub := &dnsStub{ifaces: []netInterface{{Index: 6, Alias: "Ethernet"}}, verifyOK: false}
	installDNSStub(t, stub)

	applyEncryptedDNS()

	if stub.restored != 1 {
		t.Fatalf("a DoH setup that does not resolve must be rolled back, restored %d times", stub.restored)
	}
	report := GetTuningReport()
	if len(report.Findings) == 0 || report.Findings[0].Status != tuningStatusSkipped {
		t.Fatalf("the rollback must be reported, got %+v", report.Findings)
	}

	refreshEncryptedDNSIfNetworkChanged()
	waitForDNSRefresh(t)
	if len(stub.applied) != 1 {
		t.Fatalf("a failed setup must not be retried until the network changes, applied %v", stub.applied)
	}
}

func TestRestoreIfUnwantedLeavesAWantedSetupAlone(t *testing.T) {
	stub := &dnsStub{ifaces: []netInterface{{Index: 6, Alias: "Ethernet"}}, verifyOK: true}
	installDNSStub(t, stub)

	applyEncryptedDNS()
	restoreDNSIfUnwanted()
	if stub.restored != 0 {
		t.Fatal("a late cleanup from an earlier stop must not undo DNS the user wants now")
	}

	setDNSWanted(false)
	restoreDNSIfUnwanted()
	if stub.restored != 1 {
		t.Fatalf("an unwanted setup must be restored, restored %d times", stub.restored)
	}
}

func applyEncryptedDNS() {
	setDNSWanted(true)
	ensureEncryptedDNS()
}
