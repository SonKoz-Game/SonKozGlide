package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	dnsBackupFile          = "dns_backup.json"
	dnsVerifyTimeout       = 2500 * time.Millisecond
	dnsVerifyAttempts      = 2
	dnsVerifySlowThreshold = 1200 * time.Millisecond
	dnsFindingName         = "Guvenli DNS"
)

var dnsVerifyTargets = []string{"www.microsoft.com", "www.cloudflare.com"}

type dohServer struct {
	IP       string
	Template string
}

var encryptedDNSServers = []dohServer{
	{IP: "1.1.1.1", Template: "https://cloudflare-dns.com/dns-query"},
	{IP: "8.8.8.8", Template: "https://dns.google/dns-query"},
}

var encryptedDNSServersV6 = []dohServer{
	{IP: "2606:4700:4700::1111", Template: "https://cloudflare-dns.com/dns-query"},
	{IP: "2001:4860:4860::8888", Template: "https://dns.google/dns-query"},
}

var (
	dnsMu            sync.Mutex
	dnsWanted        bool
	dnsTemplatesOn   bool
	dnsAppliedIfaces = map[int]bool{}
	dnsAttempted     bool
	dnsAttemptSig    string
	dnsRefreshBusy   atomic.Bool

	dnsVerifier        = measureSystemResolution
	dnsInterfaceLister = listGatewayInterfaces
	dnsServerApplier   = applyDNSServers
	dnsServerRestorer  = restoreDNSServers
	dnsTemplateSetter  = configureDoHTemplates
	dnsCacheFlusher    = flushSystemDNSCache
)

type dnsInterfaceBackup struct {
	Index    int    `json:"index"`
	Alias    string `json:"alias"`
	GUID     string `json:"guid"`
	Mode     string `json:"mode"`
	Servers  string `json:"servers"`
	Mode6    string `json:"mode6"`
	Servers6 string `json:"servers6"`
}

type dnsBackup struct {
	Applied    bool                 `json:"applied"`
	SavedAt    time.Time            `json:"savedAt"`
	Interfaces []dnsInterfaceBackup `json:"interfaces"`
}

func (b dnsBackup) has(index int) bool {
	for _, iface := range b.Interfaces {
		if iface.Index == index {
			return true
		}
	}
	return false
}

func dnsBackupPath() string {
	return filepath.Join(getTargetDir(), dnsBackupFile)
}

func setDNSWanted(wanted bool) {
	dnsMu.Lock()
	dnsWanted = wanted
	dnsMu.Unlock()
}

func ensureEncryptedDNS() {
	dnsMu.Lock()
	defer dnsMu.Unlock()

	if !dnsWanted {
		return
	}

	ifaces, err := dnsInterfaceLister()
	if err != nil {
		appendEngineLog("dns: interface list failed: " + err.Error())
		return
	}
	dnsAttempted = true
	dnsAttemptSig = interfaceSignature(ifaces)

	pending := make([]netInterface, 0, len(ifaces))
	for _, iface := range ifaces {
		if !dnsAppliedIfaces[iface.Index] {
			pending = append(pending, iface)
		}
	}
	if len(pending) == 0 {
		if len(ifaces) == 0 {
			appendEngineLog("dns: no active interface detected, will retry when the network changes")
		}
		return
	}

	backup, err := loadDNSBackup()
	if err != nil || !backup.Applied {
		backup = dnsBackup{SavedAt: time.Now()}
	}
	for _, iface := range pending {
		if !backup.has(iface.Index) {
			backup.Interfaces = append(backup.Interfaces, dnsBackupFor(iface))
		}
	}
	backup.Applied = true
	if err := saveDNSBackup(backup); err != nil {
		appendEngineLog("dns: backup save failed, leaving DNS untouched: " + err.Error())
		return
	}

	if !dnsTemplatesOn {
		dnsTemplateSetter(true)
		dnsTemplatesOn = true
	}

	applied := dnsServerApplier(pending)
	for index := range applied {
		dnsAppliedIfaces[index] = true
	}
	if len(dnsAppliedIfaces) == 0 {
		appendEngineLog("dns: could not apply encrypted DNS on any interface")
		restoreDNSLocked()
		return
	}
	if len(applied) == 0 {
		return
	}

	dnsCacheFlusher()

	elapsed, ok := verifyEncryptedDNS()
	if !ok {
		appendEngineLog("dns: encrypted DNS does not resolve, reverting to the original resolvers")
		sig := dnsAttemptSig
		restoreDNSLocked()
		dnsAttempted = true
		dnsAttemptSig = sig
		setDNSFinding(&TuningFinding{
			Name:   dnsFindingName,
			Status: tuningStatusSkipped,
			Detail: "cozumleme basarisiz, orijinal DNS geri alindi",
		})
		return
	}

	status := tuningStatusFixed
	detail := fmt.Sprintf("etkin (%d ms)", elapsed.Milliseconds())
	if elapsed > dnsVerifySlowThreshold {
		status = tuningStatusSkipped
		detail = fmt.Sprintf("yavas yanit (%d ms), ayarlardan kapatabilirsiniz", elapsed.Milliseconds())
		appendEngineLog(fmt.Sprintf("dns: encrypted DNS is slow (%d ms)", elapsed.Milliseconds()))
	}
	setDNSFinding(&TuningFinding{Name: dnsFindingName, Status: status, Detail: detail})
	appendEngineLog(fmt.Sprintf("dns: encrypted DNS (DoH) applied on %d interface(s)", len(applied)))
}

func dnsStepResult() (string, string) {
	dnsMu.Lock()
	applied := len(dnsAppliedIfaces)
	dnsMu.Unlock()

	tuningMu.Lock()
	var finding TuningFinding
	hasFinding := dnsFinding != nil
	if hasFinding {
		finding = *dnsFinding
	}
	tuningMu.Unlock()

	if hasFinding {
		switch {
		case finding.Status == tuningStatusFixed:
			return stepDone, "doh"
		case strings.Contains(finding.Detail, "yavas"):
			return stepDone, "slow"
		default:
			return stepFailed, "reverted"
		}
	}
	if applied > 0 {
		return stepDone, "doh"
	}
	return stepSkipped, "pending"
}

func refreshEncryptedDNSIfNetworkChanged() {
	if !dnsMu.TryLock() {
		return
	}
	wanted, attempted, sig := dnsWanted, dnsAttempted, dnsAttemptSig
	dnsMu.Unlock()

	if !wanted || !attempted {
		return
	}
	ifaces, err := dnsInterfaceLister()
	if err != nil || interfaceSignature(ifaces) == sig {
		return
	}
	if !dnsRefreshBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer dnsRefreshBusy.Store(false)
		appendEngineLog("dns: network interfaces changed, re-checking encrypted DNS")
		ensureEncryptedDNS()
	}()
}

func dnsBackupFor(iface netInterface) dnsInterfaceBackup {
	static4, static6 := staticNameServers(iface.GUID)
	return backupFromInterface(iface, static4, static6)
}

func backupFromInterface(iface netInterface, static4 string, static6 string) dnsInterfaceBackup {
	backup := dnsInterfaceBackup{
		Index:    iface.Index,
		Alias:    iface.Alias,
		GUID:     iface.GUID,
		Mode:     "dhcp",
		Servers:  serversOfFamily(iface.DNSServers, false),
		Mode6:    "dhcp",
		Servers6: serversOfFamily(iface.DNSServers, true),
	}
	if static4 != "" {
		backup.Mode = "static"
		backup.Servers = static4
	}
	if static6 != "" {
		backup.Mode6 = "static"
		backup.Servers6 = static6
	}
	return backup
}

func verifyEncryptedDNS() (time.Duration, bool) {
	var elapsed time.Duration
	for attempt := 0; attempt < dnsVerifyAttempts; attempt++ {
		took, ok := dnsVerifier()
		elapsed += took
		if ok {
			return took, true
		}
	}
	return elapsed, false
}

func measureSystemResolution() (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsVerifyTimeout)
	defer cancel()

	var resolver net.Resolver
	start := time.Now()
	for _, target := range dnsVerifyTargets {
		if addrs, err := resolver.LookupIPAddr(ctx, target); err == nil && len(addrs) > 0 {
			return time.Since(start), true
		}
	}
	return time.Since(start), false
}

func RestoreDNS() {
	setDNSWanted(false)

	dnsMu.Lock()
	defer dnsMu.Unlock()

	restoreDNSLocked()
}

func restoreDNSIfUnwanted() {
	dnsMu.Lock()
	defer dnsMu.Unlock()

	if dnsWanted {
		return
	}
	restoreDNSLocked()
}

func restoreDNSLocked() {
	setDNSFinding(nil)
	dnsAppliedIfaces = map[int]bool{}
	dnsAttempted = false
	dnsAttemptSig = ""

	backup, err := loadDNSBackup()
	if err != nil || !backup.Applied {
		if dnsTemplatesOn {
			dnsTemplateSetter(false)
			dnsTemplatesOn = false
		}
		return
	}

	dnsServerRestorer(backup.Interfaces)
	dnsTemplateSetter(false)
	dnsTemplatesOn = false
	dnsCacheFlusher()

	backup.Applied = false
	if err := saveDNSBackup(backup); err != nil {
		appendEngineLog("dns: backup state save failed: " + err.Error())
	}
	appendEngineLog("dns: original DNS settings restored")
}

func applyDNSServers(ifaces []netInterface) map[int]bool {
	dualStack := psServerList(dnsServerCSV(true))
	v4Only := psServerList(dnsServerCSV(false))

	var script strings.Builder
	for _, iface := range ifaces {
		fmt.Fprintf(&script,
			"try { Set-DnsClientServerAddress -InterfaceIndex %[1]d -ServerAddresses (%[2]s) -ErrorAction Stop; 'ok %[1]d' } "+
				"catch { try { Set-DnsClientServerAddress -InterfaceIndex %[1]d -ServerAddresses (%[3]s) -ErrorAction Stop; 'ok %[1]d' } "+
				"catch { 'fail %[1]d ' + $_.Exception.Message } }\n",
			iface.Index, dualStack, v4Only)
	}

	out, err := runPSOutput(psBatchTimeout(len(ifaces)), script.String())
	applied, failures := parseIndexResults(out)
	for _, failure := range failures {
		appendEngineLog("dns: set failed: " + failure)
	}
	if err != nil && len(applied) == 0 && len(failures) == 0 {
		appendEngineLog(fmt.Sprintf("dns: set failed: %v %s", err, strings.TrimSpace(out)))
	}
	return applied
}

func restoreDNSServers(ifaces []dnsInterfaceBackup) {
	if len(ifaces) == 0 {
		return
	}

	var script strings.Builder
	for _, iface := range ifaces {
		static := ""
		if original := psServerList(originalServerCSV(iface)); original != "" {
			static = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses (%s) -ErrorAction Stop; ", iface.Index, original)
		}
		fmt.Fprintf(&script,
			"try { Set-DnsClientServerAddress -InterfaceIndex %[1]d -ResetServerAddresses -ErrorAction Stop; %[2]s'ok %[1]d' } "+
				"catch { 'fail %[1]d ' + $_.Exception.Message }\n",
			iface.Index, static)
	}

	out, err := runPSOutput(psBatchTimeout(len(ifaces)), script.String())
	_, failures := parseIndexResults(out)
	for _, failure := range failures {
		appendEngineLog("dns: restore failed: " + failure)
	}
	if err != nil && len(failures) == 0 {
		appendEngineLog(fmt.Sprintf("dns: restore failed: %v %s", err, strings.TrimSpace(out)))
	}
}

func psBatchTimeout(count int) time.Duration {
	return 6*time.Second + time.Duration(count)*4*time.Second
}

func parseIndexResults(out string) (map[int]bool, []string) {
	ok := make(map[int]bool)
	var failures []string

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "ok "):
			if index, err := strconv.Atoi(strings.TrimSpace(line[3:])); err == nil {
				ok[index] = true
			}
		case strings.HasPrefix(line, "fail "):
			failures = append(failures, strings.TrimSpace(line[5:]))
		}
	}
	return ok, failures
}

func configureDoHTemplates(enable bool) {
	auto := "no"
	if enable {
		auto = "yes"
	}
	servers := append(append([]dohServer{}, encryptedDNSServers...), encryptedDNSServersV6...)
	for _, server := range servers {
		runHidden(4*time.Second, "netsh", "dns", "delete", "encryption", "server="+server.IP)
		runHidden(4*time.Second, "netsh", "dns", "add", "encryption",
			"server="+server.IP,
			"dohtemplate="+server.Template,
			"autoupgrade="+auto,
			"udpfallback=no",
		)
	}
}

func flushSystemDNSCache() {
	runHidden(5*time.Second, "ipconfig", "/flushdns")
}

func loadDNSBackup() (dnsBackup, error) {
	data, err := os.ReadFile(dnsBackupPath())
	if err != nil {
		return dnsBackup{}, err
	}
	var backup dnsBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return dnsBackup{}, err
	}
	return backup, nil
}

func saveDNSBackup(backup dnsBackup) error {
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return err
	}
	path := dnsBackupPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.WriteFile(path, data, 0644)
		_ = os.Remove(tmp)
	}
	return nil
}

func dnsServerCSV(includeV6 bool) string {
	ips := make([]string, 0, len(encryptedDNSServers)+len(encryptedDNSServersV6))
	for _, server := range encryptedDNSServers {
		ips = append(ips, server.IP)
	}
	if includeV6 {
		for _, server := range encryptedDNSServersV6 {
			ips = append(ips, server.IP)
		}
	}
	return strings.Join(ips, ",")
}

func originalServerCSV(iface dnsInterfaceBackup) string {
	parts := make([]string, 0, 2)
	if iface.Mode == "static" && strings.TrimSpace(iface.Servers) != "" {
		parts = append(parts, iface.Servers)
	}
	if iface.Mode6 == "static" && strings.TrimSpace(iface.Servers6) != "" {
		parts = append(parts, iface.Servers6)
	}
	return strings.Join(parts, ",")
}

func psServerList(csv string) string {
	parts := strings.Split(csv, ",")
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		ip := strings.TrimSpace(part)
		if ip == "" || !isPlausibleIP(ip) {
			continue
		}
		quoted = append(quoted, "'"+ip+"'")
	}
	return strings.Join(quoted, ",")
}

func isPlausibleIP(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}
