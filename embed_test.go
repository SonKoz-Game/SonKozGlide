package main

import (
	"io/fs"
	"runtime"
	"testing"
)

func TestEmbeddedBinarySets(t *testing.T) {
	sets := map[string][]string{
		"resources/x64": {"WinDivert.dll", "WinDivert64.sys", "cygwin1.dll", "winws.exe"},
	}
	if runtime.GOARCH == "386" {
		sets["resources/x86"] = []string{"WinDivert.dll", "WinDivert32.sys", "cygwin1.dll", "winws.exe"}
	} else if _, err := fs.Stat(embeddedFiles, "resources/x86"); err == nil {
		t.Fatal("a 64-bit build can never run on 32-bit Windows and must not carry the x86 set")
	}
	for dir, want := range sets {
		entries, err := fs.ReadDir(embeddedFiles, dir)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, e := range entries {
			got[e.Name()] = true
		}
		for _, name := range want {
			if !got[name] {
				t.Fatalf("%s is missing %s", dir, name)
			}
		}
		if len(entries) != len(want) {
			t.Fatalf("%s has unexpected files: %v", dir, entries)
		}
	}
}
