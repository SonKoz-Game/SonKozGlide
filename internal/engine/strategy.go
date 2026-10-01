package engine

import (
	"sort"
	"time"
)

type Strategy struct {
	ID      string
	TCP     []string
	QuicTTL string
}

var strategyCatalog = []Strategy{
	{
		ID:      "md5-disorder",
		TCP:     []string{"--dpi-desync=fake,multidisorder", "--dpi-desync-split-pos=1,midsld", "--dpi-desync-fooling=md5sig"},
		QuicTTL: "5",
	},
	{
		ID:      "md5-fake",
		TCP:     []string{"--dpi-desync=fake", "--dpi-desync-fooling=md5sig"},
		QuicTTL: "5",
	},
	{
		ID:      "autottl-split",
		TCP:     []string{"--dpi-desync=fake,multisplit", "--dpi-desync-split-pos=1", "--dpi-desync-autottl=2"},
		QuicTTL: "5",
	},
	{
		ID:      "ttl-fake",
		TCP:     []string{"--dpi-desync=fake", "--dpi-desync-ttl=5"},
		QuicTTL: "5",
	},
	{
		ID:      "ttl-hostfake",
		TCP:     []string{"--dpi-desync=hostfakesplit", "--dpi-desync-ttl=5"},
		QuicTTL: "5",
	},
	{
		ID:      "md5-split",
		TCP:     []string{"--dpi-desync=fake,split", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=md5sig", "--dpi-desync-ttl=4", "--hostcase"},
		QuicTTL: "4",
	},
	{
		ID:      "md5-disorder2",
		TCP:     []string{"--dpi-desync=fake,disorder2", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=md5sig", "--dpi-desync-ttl=5", "--hostcase"},
		QuicTTL: "5",
	},
	{
		ID:      "badseq-disorder",
		TCP:     []string{"--dpi-desync=fake,disorder2", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=md5sig,badseq", "--dpi-desync-ttl=6", "--hostcase"},
		QuicTTL: "6",
	},
	{
		ID:      "badseq-split",
		TCP:     []string{"--dpi-desync=fake,split2", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=md5sig,badseq", "--dpi-desync-ttl=5", "--hostcase"},
		QuicTTL: "5",
	},
	{
		ID:      "seqovl-split",
		TCP:     []string{"--dpi-desync=multisplit", "--dpi-desync-split-pos=1", "--dpi-desync-split-seqovl=681"},
		QuicTTL: "5",
	},
	{
		ID:      "plain-disorder",
		TCP:     []string{"--dpi-desync=multidisorder", "--dpi-desync-split-pos=1,midsld"},
		QuicTTL: "5",
	},
}

var ispPreferredStrategy = map[ISPProfile]string{
	ISPTurkTelekom: "md5-disorder",
	ISPSuperonline: "md5-split",
	ISPVodafone:    "badseq-disorder",
	ISPGeneric:     "md5-disorder2",
}

func strategyByID(id string) (Strategy, bool) {
	for _, s := range strategyCatalog {
		if s.ID == id {
			return s, true
		}
	}
	return Strategy{}, false
}

func isKnownStrategy(id string) bool {
	_, ok := strategyByID(id)
	return ok
}

func preferredStrategy(profile ISPProfile) string {
	return ispPreferredStrategy[profile]
}

func buildArgs(s Strategy) []string {
	args := []string{
		"--wf-tcp=443",
		"--wf-udp=443",
		"--filter-tcp=443",
		"--hostlist=list.txt",
	}
	args = append(args, s.TCP...)
	return append(args, quicArgs(s.QuicTTL)...)
}

func quicArgs(ttl string) []string {
	if ttl == "" {
		ttl = "5"
	}
	return []string{
		"--new",
		"--filter-udp=443",
		"--filter-l7=quic",
		"--hostlist=list.txt",
		"--dpi-desync=fake",
		"--dpi-desync-repeats=2",
		"--dpi-desync-cutoff=n2",
		"--dpi-desync-ttl=" + ttl,
	}
}

func strategyOrder(preferred string, demoted string) []Strategy {
	adaptiveMu.Lock()
	state := loadAdaptiveStateLocked()
	adaptiveMu.Unlock()

	now := time.Now()
	type ranked struct {
		strategy Strategy
		score    float64
	}
	list := make([]ranked, 0, len(strategyCatalog))
	for order, s := range strategyCatalog {
		stats, hasStats := state.ProfileStats[s.ID]
		list = append(list, ranked{
			strategy: s,
			score:    adaptiveProfileScore(stats, hasStats, order, now, state.LastProfile == s.ID),
		})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].score < list[j].score })

	result := make([]Strategy, 0, len(list))
	if s, ok := strategyByID(preferred); ok && preferred != demoted {
		result = append(result, s)
	}
	var tail []Strategy
	for _, item := range list {
		switch item.strategy.ID {
		case preferred:
			if preferred != demoted {
				continue
			}
			tail = append(tail, item.strategy)
		case demoted:
			tail = append(tail, item.strategy)
		default:
			result = append(result, item.strategy)
		}
	}
	return append(result, tail...)
}

func LearnedStrategy() (string, float64, bool) {
	adaptiveMu.Lock()
	state := loadAdaptiveStateLocked()
	adaptiveMu.Unlock()

	best := ""
	bestScore := 0.0
	for _, s := range strategyCatalog {
		stats, ok := state.ProfileStats[s.ID]
		if !ok || stats.LastSuccessAt.IsZero() {
			continue
		}
		score := adaptiveProfileScore(stats, true, 0, time.Now(), state.LastProfile == s.ID)
		if best == "" || score < bestScore {
			best = s.ID
			bestScore = score
		}
	}
	if best == "" {
		return "", 0, false
	}
	return best, clamp01(state.ProfileStats[best].SuccessEWMA), true
}
