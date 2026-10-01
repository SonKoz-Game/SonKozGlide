package updater

import "testing"

func TestGetVersionNormalisesTheInjectedValue(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	for raw, want := range map[string]string{
		"1.1.0":    "v1.1.0",
		"v1.1.0":   "v1.1.0",
		" v1.1.0 ": "v1.1.0",
	} {
		Version = raw
		if got := GetVersion(); got != want {
			t.Fatalf("GetVersion() with Version=%q = %q; want %q", raw, got, want)
		}
	}
}

func TestGetVersionSurvivesAMissingBuildFlag(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	for _, raw := range []string{"", "   "} {
		Version = raw
		if got := GetVersion(); got != fallbackVersion {
			t.Fatalf("an un-injected build must fall back, got %q", got)
		}
	}
}
