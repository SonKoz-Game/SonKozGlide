package glide

import "strings"

const defaultStrategyID = "ttl-split"

type desyncMode int

const (
	modeSplit desyncMode = iota
	modeFakeTTL
)

type strategy struct {
	ID       string
	Mode     desyncMode
	TTL      int
	SplitPos int
	SplitSNI bool
}

var strategyCatalog = []strategy{
	{ID: "ttl-split", Mode: modeFakeTTL, TTL: 5, SplitPos: 1, SplitSNI: true},
	{ID: "ttl-fake", Mode: modeFakeTTL, TTL: 5, SplitSNI: true},
	{ID: "ttl-low", Mode: modeFakeTTL, TTL: 3, SplitSNI: true},
	{ID: "ttl-high", Mode: modeFakeTTL, TTL: 8, SplitSNI: true},
	{ID: "split-sni", Mode: modeSplit, SplitPos: 1, SplitSNI: true},
	{ID: "split-pos", Mode: modeSplit, SplitPos: 2},
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
