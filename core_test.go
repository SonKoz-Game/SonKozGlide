package main

import (
	"encoding/xml"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func countTrims(t *testing.T) *atomic.Int32 {
	t.Helper()
	var trims atomic.Int32
	previousDelay, previousTrim := idleTrimDelay, trimFn
	idleTrimDelay = 20 * time.Millisecond
	trimFn = func() { trims.Add(1) }
	t.Cleanup(func() {
		idleTrimDelay, trimFn = previousDelay, previousTrim
	})
	return &trims
}

func TestIdleTrimWaitsUntilTheWindowIsGone(t *testing.T) {
	trims := countTrims(t)
	c := NewCore(embeddedFiles, nil)

	c.ui = &uiProcess{}
	c.trimWhenIdle()
	time.Sleep(80 * time.Millisecond)
	if n := trims.Load(); n != 0 {
		t.Fatalf("trimmed %d times while the window was open", n)
	}

	c.uiMu.Lock()
	c.ui = nil
	c.uiMu.Unlock()
	c.trimWhenIdle()
	time.Sleep(80 * time.Millisecond)
	if n := trims.Load(); n != 1 {
		t.Fatalf("trimmed %d times after the window closed, want 1", n)
	}
}

func TestIdleTrimCollapsesBurstsIntoOne(t *testing.T) {
	trims := countTrims(t)
	c := NewCore(embeddedFiles, nil)

	for i := 0; i < 5; i++ {
		c.trimWhenIdle()
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	if n := trims.Load(); n != 1 {
		t.Fatalf("trimmed %d times for one idle period, want 1", n)
	}
}

func TestIdleTrimSkipsWhileQuitting(t *testing.T) {
	trims := countTrims(t)
	c := NewCore(embeddedFiles, nil)

	c.trimWhenIdle()
	c.closeUI()
	time.Sleep(80 * time.Millisecond)
	if n := trims.Load(); n != 0 {
		t.Fatalf("trimmed %d times during shutdown", n)
	}
}

func TestAutoStartTaskXMLRunsAtNormalPriorityWithoutLimits(t *testing.T) {
	doc := autoStartTaskXML(`C:\Oyun & Araç\SonKozGlide.exe`, "S-1-5-21-1-2-3-1001")

	var task struct {
		Settings struct {
			Priority           int    `xml:"Priority"`
			ExecutionTimeLimit string `xml:"ExecutionTimeLimit"`
			DisallowOnBattery  bool   `xml:"DisallowStartIfOnBatteries"`
			StopOnBattery      bool   `xml:"StopIfGoingOnBatteries"`
		} `xml:"Settings"`
		Command string `xml:"Actions>Exec>Command"`
		User    string `xml:"Triggers>LogonTrigger>UserId"`
	}
	if err := xml.NewDecoder(strings.NewReader(strings.Replace(doc, "UTF-16", "UTF-8", 1))).Decode(&task); err != nil {
		t.Fatal(err)
	}
	if task.Settings.Priority != 4 || task.Settings.ExecutionTimeLimit != "PT0S" {
		t.Fatalf("the default task priority 7 runs the app below normal at logon and kills it after 72h, got %+v", task.Settings)
	}
	if task.Settings.DisallowOnBattery || task.Settings.StopOnBattery {
		t.Fatal("a laptop on battery must still start the app")
	}
	if task.Command != `C:\Oyun & Araç\SonKozGlide.exe` || task.User != "S-1-5-21-1-2-3-1001" {
		t.Fatalf("got command %q user %q", task.Command, task.User)
	}
	if !commandReferencesExecutable(doc, xmlEscape(`C:\Oyun & Araç\SonKozGlide.exe`)) {
		t.Fatal("an installed task must be recognised from its XML even when the path needs escaping")
	}
}

func TestUTF16FileHasBOM(t *testing.T) {
	out := utf16File("<a>ç</a>")
	if out[0] != 0xFF || out[1] != 0xFE || len(out) != 2+2*8 {
		t.Fatalf("got % x", out)
	}
}
