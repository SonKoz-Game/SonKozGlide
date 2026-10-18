package glide

import "strings"

const defaultStrategyID = "ttl-split"

type desyncMode int

const (
	modeSplit desyncMode = iota
	modeFakeTTL
	modeFakeSplit
)

type strategy struct {
	ID       string
	Mode     desyncMode
	TTL      int
	SplitPos int
	SplitSNI bool
}

var strategyCatalog = []strategy{
	{ID: "ttl-split", Mode: modeFakeSplit, TTL: 5, SplitPos: 1, SplitSNI: true},
	{ID: "ttl-split-low", Mode: modeFakeSplit, TTL: 3, SplitPos: 1, SplitSNI: true},
	{ID: "ttl-split-mid", Mode: modeFakeSplit, TTL: 4, SplitPos: 1, SplitSNI: true},
	{ID: "ttl-split-high", Mode: modeFakeSplit, TTL: 6, SplitPos: 1, SplitSNI: true},
	{ID: "ttl-fake", Mode: modeFakeTTL, TTL: 5, SplitSNI: true},
	{ID: "ttl-fake-low", Mode: modeFakeTTL, TTL: 2, SplitSNI: true},
	{ID: "ttl-fake-high", Mode: modeFakeTTL, TTL: 7, SplitSNI: true},
	{ID: "split-sni", Mode: modeSplit, SplitPos: 1, SplitSNI: true},
	{ID: "split-pos", Mode: modeSplit, SplitPos: 2},
	{ID: "split-pos3", Mode: modeSplit, SplitPos: 3},
}

func strategyByID(id string) (strategy, bool) {
	for _, s := range strategyCatalog {
		if s.ID == id {
			return s, true
		}
	}
	return strategy{}, false
}

func normalizeStrategy(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return defaultStrategyID
	}
	if _, ok := strategyByID(id); ok {
		return id
	}
	return defaultStrategyID
}

func strategyOrder(preferred string) []strategy {
	var head strategy
	hasHead := false
	if s, ok := strategyByID(preferred); ok {
		head = s
		hasHead = true
	}
	out := make([]strategy, 0, len(strategyCatalog))
	if hasHead {
		out = append(out, head)
	}
	for _, s := range strategyCatalog {
		if hasHead && s.ID == head.ID {
			continue
		}
		out = append(out, s)
	}
	return out
}
