package router

import "strings"

type MatchKind uint8

const (
	MatchExact MatchKind = iota
	MatchSuffix
	MatchWildcard
)

type Counts struct {
	Domains   int
	Keywords  int
	Regexes   int
	CIDRs     int
	Platforms int
}

type domainNode struct {
	children map[string]*domainNode
	exact    bool
	suffix   bool
	wildcard bool
}

type domainTrie struct {
	root *domainNode
	size int
}

type RuleSet struct {
	Version       string
	PlatformNames []string
	domains       *domainTrie
	Counts        Counts
}

func newDomainTrie() *domainTrie { return &domainTrie{root: &domainNode{}} }

func labels(host string) []string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return nil
	}
	return strings.Split(host, ".")
}

func (t *domainTrie) insert(host string, kind MatchKind) {
	ls := labels(host)
	if len(ls) == 0 {
		return
	}

	n := t.root
	for i := len(ls) - 1; i >= 0; i-- {
		lbl := ls[i]
		child := n.children[lbl]
		if child == nil {
			if n.children == nil {
				n.children = make(map[string]*domainNode, 2)
			}
			child = &domainNode{}
			n.children[lbl] = child
		}
		n = child
	}

	switch kind {
	case MatchExact:
		n.exact = true
	case MatchWildcard:
		n.wildcard = true
	default:
		n.suffix = true
	}
	t.size++
}

func (n *domainNode) isRule() bool {
	return n.exact || n.suffix || n.wildcard
}
