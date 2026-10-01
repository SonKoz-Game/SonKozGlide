package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tuningBackupFile    = "net_tuning.json"
	mtuOverhead         = 28
	mtuFloor            = 1280
	mtuAutoFloor        = 1400
	mtuPingTimeoutMs    = 1000
	mtuPingBudget       = 4 * time.Second
	preferredAutoTuning = "normal"
	tuningStatusOK      = "ok"
	tuningStatusFixed   = "fixed"
	tuningStatusSkipped = "skipped"
)

var mtuCandidates = []int{1500, 1492, 1480, 1460, 1452, 1420, 1400, 1360, 1300, 1280}

var mtuProbeTargets = []string{"1.1.1.1", "8.8.8.8"}

var validAutoTuningLevels = map[string]bool{
	"disabled":         true,
	"highlyrestricted": true,
	"restricted":       true,
	"normal":           true,
	"experimental":     true,
}

var (
	tuningMu          sync.Mutex
	tuningWanted      bool
	tuningDone        bool
	tuningReport      TuningReport
	dnsFinding        *TuningFinding
	pingProbe         = pingWithDontFragment
	plainPingProbe    = pingWithoutDontFragment
	autoTuningReader  = readAutoTuningLevel
	autoTuningSetter  = setAutoTuningLevel
	tuningIfaceLister = captureTuningInterfaces
)

type TuningFinding struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type TuningReport struct {
	RanAt    time.Time       `json:"ranAt"`
	Findings []TuningFinding `json:"findings"`
}

type tuningInterfaceBackup struct {
	Index int    `json:"index"`
	Alias string `json:"alias"`
	MTU   int    `json:"mtu"`
}

type tuningBackup struct {
	Applied    bool                    `json:"applied"`
	SavedAt    time.Time               `json:"savedAt"`
	AutoTuning string                  `json:"autoTuning"`
	Interfaces []tuningInterfaceBackup `json:"interfaces"`
}

func tuningBackupPath() string {
	return filepath.Join(getTargetDir(), tuningBackupFile)
}

func setTuningWanted(wanted bool) {
	tuningMu.Lock()
	tuningWanted = wanted
	tuningMu.Unlock()
}

func ensureNetworkTuning() {
	tuningMu.Lock()
	defer tuningMu.Unlock()

	if !tuningWanted || tuningDone {
		return
	}

	backup, err := loadTuningBackup()
	if err != nil || !backup.Applied {
		backup = tuningBackup{SavedAt: time.Now()}
	}

	findings := []TuningFinding{applyAutoTuning(&backup)}
	findings = append(findings, applyPathMTU(&backup)...)

	if backup.AutoTuning != "" || len(backup.Interfaces) > 0 {
		backup.Applied = true
		if err := saveTuningBackup(backup); err != nil {
			appendEngineLog("tuning: backup save failed: " + err.Error())
		}
	}

	tuningReport = TuningReport{RanAt: time.Now(), Findings: findings}
	tuningDone = true
}

func RestoreNetworkTuning() {
	setTuningWanted(false)

	tuningMu.Lock()
	defer tuningMu.Unlock()

	restoreNetworkTuningLocked()
}

func restoreTuningIfUnwanted() {
	tuningMu.Lock()
	defer tuningMu.Unlock()

	if tuningWanted {
		return
	}
	restoreNetworkTuningLocked()
}

func restoreNetworkTuningLocked() {
	tuningDone = false

	backup, err := loadTuningBackup()
	if err != nil || !backup.Applied {
		return
	}

	if backup.AutoTuning != "" {
		setAutoTuningLevel(backup.AutoTuning)
	}
	for _, iface := range backup.Interfaces {
		setInterfaceMTU(iface.Index, iface.MTU)
	}

	backup.Applied = false
	backup.AutoTuning = ""
	backup.Interfaces = nil
	if err := saveTuningBackup(backup); err != nil {
		appendEngineLog("tuning: backup state save failed: " + err.Error())
	}

	tuningReport = TuningReport{}
	appendEngineLog("tuning: original network settings restored")
}

func GetTuningReport() TuningReport {
	tuningMu.Lock()
	defer tuningMu.Unlock()

	report := TuningReport{RanAt: tuningReport.RanAt}
	if dnsFinding != nil {
		report.Findings = append(report.Findings, *dnsFinding)
	}
	report.Findings = append(report.Findings, tuningReport.Findings...)
	return report
}

func tuningStepResult() (string, string) {
	tuningMu.Lock()
	defer tuningMu.Unlock()

	fixed, ok := 0, 0
	for _, finding := range tuningReport.Findings {
		switch finding.Status {
		case tuningStatusFixed:
			fixed++
		case tuningStatusOK:
			ok++
		}
	}
	switch {
	case fixed > 0:
		return stepDone, fmt.Sprintf("fixed:%d", fixed)
	case ok > 0:
		return stepDone, "ok"
	default:
		return stepSkipped, "unknown"
	}
}

func setDNSFinding(finding *TuningFinding) {
	tuningMu.Lock()
	dnsFinding = finding
	tuningMu.Unlock()
}

func applyAutoTuning(backup *tuningBackup) TuningFinding {
	const name = "TCP pencere olcekleme"

	level, ok := autoTuningReader()
	if !ok {
		return TuningFinding{Name: name, Status: tuningStatusSkipped, Detail: "durum okunamadi"}
	}
	if level == preferredAutoTuning {
		return TuningFinding{Name: name, Status: tuningStatusOK, Detail: level}
	}

	recorded := backup.AutoTuning
	if recorded == "" {
		backup.AutoTuning = level
	}
	if !autoTuningSetter(preferredAutoTuning) {
		if recorded == "" {
			backup.AutoTuning = ""
		}
		return TuningFinding{Name: name, Status: tuningStatusSkipped, Detail: level + " (degistirilemedi)"}
	}

	appendEngineLog(fmt.Sprintf("tuning: tcp autotuning %s -> %s", level, preferredAutoTuning))
	return TuningFinding{Name: name, Status: tuningStatusFixed, Detail: level + " -> " + preferredAutoTuning}
}

func applyPathMTU(backup *tuningBackup) []TuningFinding {
	const name = "MTU"

	pathMTU, ok := probePathMTU()
	if !ok {
		return []TuningFinding{{Name: name, Status: tuningStatusSkipped, Detail: "guvenilir olcum yok, degistirilmedi"}}
	}

	interfaces, err := tuningIfaceLister()
	if err != nil || len(interfaces) == 0 {
		return []TuningFinding{{Name: name, Status: tuningStatusSkipped, Detail: "arayuz bulunamadi"}}
	}

	findings := make([]TuningFinding, 0, len(interfaces))
	for _, iface := range interfaces {
		detail := fmt.Sprintf("%s: %d", iface.Alias, iface.MTU)

		if iface.MTU <= 0 || iface.MTU <= pathMTU {
			findings = append(findings, TuningFinding{Name: name, Status: tuningStatusOK, Detail: detail})
			continue
		}
		if !setInterfaceMTU(iface.Index, pathMTU) {
			findings = append(findings, TuningFinding{Name: name, Status: tuningStatusSkipped, Detail: detail + " (degistirilemedi)"})
			continue
		}
		if !internetReachable(context.Background()) {
			setInterfaceMTU(iface.Index, iface.MTU)
			findings = append(findings, TuningFinding{Name: name, Status: tuningStatusSkipped, Detail: detail + " (geri alindi)"})
			continue
		}

		backup.Interfaces = upsertInterfaceBackup(backup.Interfaces, iface)
		appendEngineLog(fmt.Sprintf("tuning: mtu %s %d -> %d", iface.Alias, iface.MTU, pathMTU))
		findings = append(findings, TuningFinding{
			Name:   name,
			Status: tuningStatusFixed,
			Detail: fmt.Sprintf("%s: %d -> %d", iface.Alias, iface.MTU, pathMTU),
		})
	}
	return findings
}

func upsertInterfaceBackup(existing []tuningInterfaceBackup, iface tuningInterfaceBackup) []tuningInterfaceBackup {
	for _, saved := range existing {
		if saved.Index == iface.Index {
			return existing
		}
	}
	return append(existing, iface)
}

func probePathMTU() (int, bool) {
	target, ok := reachablePingTarget()
	if !ok {
		return 0, false
	}

	smallestRejected := 0
	for _, mtu := range mtuCandidates {
		if mtu < mtuAutoFloor {
			break
		}
		if !pingProbe(target, mtu-mtuOverhead) {
			smallestRejected = mtu
			continue
		}
		if smallestRejected == 0 {
			return mtu, true
		}
		if !fragmentationConfirmed(target, smallestRejected) {
			return 0, false
		}
		return mtu, true
	}
	return 0, false
}

func fragmentationConfirmed(target string, mtu int) bool {
	return plainPingProbe(target, mtu-mtuOverhead)
}

func reachablePingTarget() (string, bool) {
	for _, target := range mtuProbeTargets {
		if pingProbe(target, mtuFloor-mtuOverhead) {
			return target, true
		}
	}
	return "", false
}

func pingWithDontFragment(target string, payload int) bool {
	return pingSized(target, payload, true)
}

func pingWithoutDontFragment(target string, payload int) bool {
	return pingSized(target, payload, false)
}

func pingSized(target string, payload int, dontFragment bool) bool {
	if payload <= 0 {
		return false
	}

	args := []string{"-n", "1"}
	if dontFragment {
		args = append(args, "-f")
	}
	args = append(args, "-l", strconv.Itoa(payload), "-w", strconv.Itoa(mtuPingTimeoutMs), target)

	if runHiddenStatus(mtuPingBudget, "ping", args...) {
		return true
	}
	return runHiddenStatus(mtuPingBudget, "ping", args...)
}

func readAutoTuningLevel() (string, bool) {
	out, err := runPSOutput(8*time.Second, "(Get-NetTCPSetting -SettingName Internet -ErrorAction SilentlyContinue).AutoTuningLevelLocal")
	if err != nil {
		return "", false
	}
	return normalizeAutoTuningLevel(out)
}

func normalizeAutoTuningLevel(raw string) (string, bool) {
	level := strings.ToLower(strings.TrimSpace(raw))
	if !validAutoTuningLevels[level] {
		return "", false
	}
	return level, true
}

func setAutoTuningLevel(level string) bool {
	normalized, ok := normalizeAutoTuningLevel(level)
	if !ok {
		return false
	}
	return runHiddenStatus(8*time.Second, "netsh", "int", "tcp", "set", "global", "autotuninglevel="+normalized)
}

func setInterfaceMTU(index int, mtu int) bool {
	if index <= 0 || mtu < mtuFloor {
		return false
	}
	return runHiddenStatus(8*time.Second, "netsh", "interface", "ipv4", "set", "subinterface",
		strconv.Itoa(index), "mtu="+strconv.Itoa(mtu), "store=active")
}

func captureTuningInterfaces() ([]tuningInterfaceBackup, error) {
	ifaces, err := listGatewayInterfaces()
	if err != nil {
		return nil, err
	}
	return tuningInterfacesFrom(ifaces), nil
}

func tuningInterfacesFrom(ifaces []netInterface) []tuningInterfaceBackup {
	var out []tuningInterfaceBackup
	for _, iface := range ifaces {
		if !iface.HasIPv4Gateway {
			continue
		}
		out = append(out, tuningInterfaceBackup{Index: iface.Index, Alias: iface.Alias, MTU: iface.MTU})
	}
	return out
}

func loadTuningBackup() (tuningBackup, error) {
	data, err := os.ReadFile(tuningBackupPath())
	if err != nil {
		return tuningBackup{}, err
	}
	var backup tuningBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return tuningBackup{}, err
	}
	return backup, nil
}

func saveTuningBackup(backup tuningBackup) error {
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return err
	}
	path := tuningBackupPath()
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
