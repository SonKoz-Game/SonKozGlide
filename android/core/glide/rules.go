package glide

import (
	"strings"
)

type ruleSet struct {
	exact    map[string]struct{}
	suffixes []string
}

var defaultDomains = []string{
	"discord.com",
	"discordapp.com",
	"discord.gg",
	"discord.media",
	"discordapp.net",
	"discordcdn.com",
	"discordstatus.com",
	"dis.gd",
	"discord.co",
	"roblox.com",
	"rbxcdn.com",
	"rbxgcdn.com",
	"rbximg.com",
	"rbxtok.com",
	"rbxasm.com",
	"rbxmgr.com",
	"rbxapidev.com",
	"robloxlabs.com",
}

func parseRules(domains string) (*ruleSet, error) {
	list := splitDomains(domains)
	if len(list) == 0 {
		list = defaultDomains
	}
	rs := &ruleSet{exact: make(map[string]struct{}, len(list))}
	for _, raw := range list {
		d := strings.ToLower(strings.TrimSpace(raw))
		d = strings.TrimPrefix(d, "*.")
		d = strings.Trim(d, ".")
		if d == "" {
			continue
		}
		rs.exact[d] = struct{}{}
		rs.suffixes = append(rs.suffixes, "."+d)
	}
	return rs, nil
}

func (rs *ruleSet) match(name string) bool {
	n := strings.ToLower(strings.Trim(name, "."))
	if n == "" {
		return false
	}
	if _, ok := rs.exact[n]; ok {
		return true
	}
	for _, suffix := range rs.suffixes {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

func (rs *ruleSet) count() int {
	return len(rs.exact)
}

func splitDomains(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == ',' || r == ' ' || r == '\t' || r == '\r'
	})
	return fields
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
