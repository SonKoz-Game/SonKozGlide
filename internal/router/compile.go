package router

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ruleDoc struct {
	Version   string                 `yaml:"version"`
	Platforms map[string]platformDoc `yaml:"platforms"`
}

type platformDoc struct {
	Aliases    []string               `yaml:"aliases"`
	Categories map[string]categoryDoc `yaml:"categories"`
}

type categoryDoc struct {
	Exact     []string `yaml:"exact"`
	Domains   []string `yaml:"domains"`
	Wildcards []string `yaml:"wildcards"`
	Keywords  []string `yaml:"keywords"`
	Regex     []string `yaml:"regex"`
	IPCIDR    []string `yaml:"ip_cidr"`
	ASN       []string `yaml:"asn"`
	Note      string   `yaml:"note"`
}

func Compile(data []byte) (*RuleSet, error) {
	var doc ruleDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("ruleset parse: %w", err)
	}

	rs := &RuleSet{
		Version: doc.Version,
		domains: newDomainTrie(),
	}

	keywords := 0
	regexes := 0
	cidrs := 0

	for platform, pd := range doc.Platforms {
		for category, cd := range pd.Categories {
			for _, h := range cd.Exact {
				rs.domains.insert(h, MatchExact)
			}
			for _, h := range cd.Domains {
				rs.domains.insert(h, MatchSuffix)
			}
			for _, w := range cd.Wildcards {
				rs.domains.insert(strings.TrimPrefix(w, "*."), MatchWildcard)
			}
			for _, kw := range cd.Keywords {
				if strings.TrimSpace(kw) == "" {
					continue
				}
				keywords++
			}
			for _, pat := range cd.Regex {
				if _, err := regexp.Compile(pat); err != nil {
					return nil, fmt.Errorf("regex %q (%s/%s): %w", pat, platform, category, err)
				}
				regexes++
			}
			for _, c := range cd.IPCIDR {
				if _, err := netip.ParsePrefix(strings.TrimSpace(c)); err != nil {
					return nil, fmt.Errorf("cidr %q (%s/%s): %w", c, platform, category, err)
				}
				cidrs++
			}
		}
	}

	rs.Counts = Counts{
		Domains:   rs.domains.size,
		Keywords:  keywords,
		Regexes:   regexes,
		CIDRs:     cidrs,
		Platforms: len(doc.Platforms),
	}
	for platform := range doc.Platforms {
		rs.PlatformNames = append(rs.PlatformNames, platform)
	}
	sort.Strings(rs.PlatformNames)
	return rs, nil
}

func (rs *RuleSet) HostlistProjection() []string {
	seen := make(map[string]struct{})
	var all []string

	var collect func(n *domainNode, path []string)
	collect = func(n *domainNode, path []string) {
		if n.isRule() {
			d := strings.Join(reverse(path), ".")
			if _, ok := seen[d]; !ok {
				seen[d] = struct{}{}
				all = append(all, d)
			}
		}
		for lbl, child := range n.children {
			next := make([]string, len(path), len(path)+1)
			copy(next, path)
			collect(child, append(next, lbl))
		}
	}
	collect(rs.domains.root, nil)

	sort.Slice(all, func(i, j int) bool {
		li, lj := strings.Count(all[i], "."), strings.Count(all[j], ".")
		if li != lj {
			return li < lj
		}
		return all[i] < all[j]
	})

	kept := make([]string, 0, len(all))
	for _, d := range all {
		if coveredByParent(d, kept) {
			continue
		}
		kept = append(kept, d)
	}
	sort.Strings(kept)
	return kept
}

func WriteHostlist(path string, rs *RuleSet) error {
	lines := rs.HostlistProjection()
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return writeFileAtomic(path, []byte(content))
}

func coveredByParent(domain string, parents []string) bool {
	for _, p := range parents {
		if domain == p || strings.HasSuffix(domain, "."+p) {
			return true
		}
	}
	return false
}

func reverse(in []string) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
