package main

import (
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
