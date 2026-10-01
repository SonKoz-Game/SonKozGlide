package engine

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

type BootFile struct {
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes"`
	Updated bool   `json:"updated"`
}

type BootInfo struct {
	Components    []BootFile `json:"components"`
	PlatformNames []string   `json:"platformNames"`
	Files         int        `json:"files"`
	FilesUpdated  int        `json:"filesUpdated"`
	SetupMs       int64      `json:"setupMs"`
	DriverReady   bool       `json:"driverReady"`
	RulesVersion  string     `json:"rulesVersion"`
	Domains       int        `json:"domains"`
	Platforms     int        `json:"platforms"`
	HostlistLines int        `json:"hostlistLines"`
	RulesMs       int64      `json:"rulesMs"`
	Adapter       string     `json:"adapter"`
	MTU           int        `json:"mtu"`
	Adapters      int        `json:"adapters"`
	NetworkMs     int64      `json:"networkMs"`
	Strategy      string     `json:"strategy"`
	StrategyScore float64    `json:"strategyScore"`
	Strategies    int        `json:"strategies"`
}

var (
	bootMu    sync.Mutex
	bootStats BootInfo
)

func recordSetupStats(components []BootFile, updated int, took time.Duration) {
	bootMu.Lock()
	bootStats.Components = components
	bootStats.Files = len(components)
	bootStats.FilesUpdated = updated
	bootStats.SetupMs = took.Milliseconds()
	bootMu.Unlock()
}

func recordRulesetStats(version string, domains int, platforms []string, lines int, took time.Duration) {
	bootMu.Lock()
	bootStats.RulesVersion = version
	bootStats.Domains = domains
	bootStats.PlatformNames = platforms
	bootStats.Platforms = len(platforms)
	bootStats.HostlistLines = lines
	bootStats.RulesMs = took.Milliseconds()
	bootMu.Unlock()
}

func GetBootInfo() BootInfo {
	bootMu.Lock()
	info := bootStats
	info.Components = append([]BootFile(nil), bootStats.Components...)
	info.PlatformNames = append([]string(nil), bootStats.PlatformNames...)
	bootMu.Unlock()

	_, err := os.Stat(filepath.Join(getTargetDir(), "WinDivert64.sys"))
	info.DriverReady = err == nil

	start := time.Now()
	if ifaces, err := listGatewayInterfaces(); err == nil {
		for _, iface := range ifaces {
			if !iface.HasIPv4Gateway {
				continue
			}
			info.Adapters++
			if info.Adapter == "" {
				info.Adapter = iface.Alias
				info.MTU = iface.MTU
			}
		}
	}
	info.NetworkMs = time.Since(start).Milliseconds()

	info.Strategies = len(strategyCatalog)
	if id, score, ok := LearnedStrategy(); ok {
		info.Strategy = id
		info.StrategyScore = score
	}
	return info
}
