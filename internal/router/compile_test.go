package router

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleRules = `
version: "test-1"
platforms:
  discord:
    categories:
      auth_api:
        domains: [discord.com, discordapp.com]
      media_cdn:
        domains: [discordapp.net]
      voice:
        ip_cidr: [66.22.192.0/18]
  openai:
    categories:
      user_content:
        domains: [oaiusercontent.com]
      auth:
        wildcards: ["*.auth0.com"]
      cdn:
        keywords: [oaistatic]
      pin:
        exact: [status.openai.com]
`

func TestCompileCountsEveryRuleKind(t *testing.T) {
	rs, err := Compile([]byte(sampleRules))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if rs.Version != "test-1" {
		t.Fatalf("version=%q", rs.Version)
	}
	if rs.Counts.Domains != 6 {
		t.Fatalf("expected 6 domain rules, got %d", rs.Counts.Domains)
	}
	if rs.Counts.Keywords != 1 || rs.Counts.CIDRs != 1 {
		t.Fatalf("unexpected counts: %+v", rs.Counts)
	}
}

func TestCompileRejectsBrokenRules(t *testing.T) {
	cases := map[string]string{
		"bad cidr":  "version: t\nplatforms:\n  a: { categories: { c: { ip_cidr: [not-a-cidr] } } }\n",
		"bad regex": "version: t\nplatforms:\n  a: { categories: { c: { regex: [\"([\"] } } }\n",
		"bad yaml":  "version: [\n",
	}

	for name, doc := range cases {
		if _, err := Compile([]byte(doc)); err == nil {
			t.Errorf("%s: expected the whole ruleset to be rejected", name)
		}
	}
}

func TestHostlistProjectionCollapsesRedundantEntries(t *testing.T) {
	rs, err := Compile([]byte(`
version: t
platforms:
  d:
    categories:
      c:
        domains: [discord.com]
        exact: [api.discord.com]
        wildcards: ["*.cdn.discord.com"]
      m:
        domains: [discordapp.net]
`))
	if err != nil {
		t.Fatal(err)
	}

	got := rs.HostlistProjection()
	want := []string{"discord.com", "discordapp.net"}

	if len(got) != len(want) {
		t.Fatalf("projection=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("projection=%v want %v", got, want)
		}
	}
}

func TestHostlistProjectionIsSortedAndDeduplicated(t *testing.T) {
	rs, err := Compile([]byte(`
version: t
platforms:
  a:
    categories:
      c:
        domains: [zeta.com, alpha.com, alpha.com]
        exact: [alpha.com]
`))
	if err != nil {
		t.Fatal(err)
	}

	got := rs.HostlistProjection()
	if len(got) != 2 || got[0] != "alpha.com" || got[1] != "zeta.com" {
		t.Fatalf("expected a sorted, deduplicated projection, got %v", got)
	}
}

func TestWriteHostlistReplacesTheFileAtomically(t *testing.T) {
	rs, err := Compile([]byte("version: t\nplatforms:\n  a: { categories: { c: { domains: [example.com] } } }\n"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "list.txt")
	if err := os.WriteFile(path, []byte("stale\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteHostlist(path, rs); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "example.com\n" {
		t.Fatalf("unexpected hostlist content: %q", data)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("the temporary file must not survive a successful write")
	}
}

func TestDomainTrieIgnoresEmptyHosts(t *testing.T) {
	trie := newDomainTrie()
	for _, host := range []string{"", "   ", "."} {
		trie.insert(host, MatchSuffix)
	}

	if trie.size != 0 {
		t.Fatalf("expected empty hosts to be ignored, size=%d", trie.size)
	}
}

func TestDomainTrieNormalisesHosts(t *testing.T) {
	rs, err := Compile([]byte("version: t\nplatforms:\n  a: { categories: { c: { domains: [\"  Discord.COM.  \"] } } }\n"))
	if err != nil {
		t.Fatal(err)
	}

	got := rs.HostlistProjection()
	if len(got) != 1 || got[0] != "discord.com" {
		t.Fatalf("expected a normalised host, got %v", got)
	}
}
