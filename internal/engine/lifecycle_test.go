package engine

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type opRecorder struct {
	mu    sync.Mutex
	calls []string
	block chan struct{}
	err   error
}

func (r *opRecorder) record(name string) {
	r.mu.Lock()
	r.calls = append(r.calls, name)
	block := r.block
	r.mu.Unlock()

	if block != nil {
		<-block
	}
}

func (r *opRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func installOpRecorder(t *testing.T, rec *opRecorder) {
	t.Helper()

	oldStart, oldHalt, oldRestore := startFn, haltFn, restoreFn
	startFn = func() error {
		rec.record("start")
		return rec.err
	}
	haltFn = func() { rec.record("stop") }
	restoreFn = func() { rec.record("restore") }

	t.Cleanup(func() {
		waitForIdleWorker(t)
		startFn, haltFn, restoreFn = oldStart, oldHalt, oldRestore

		opMu.Lock()
		opDesired, opRestart, opWorking = false, false, false
		opMu.Unlock()

		mu.Lock()
		currentPhase, phaseDetail, statusCallback, errorCallback = PhaseIdle, "", nil, nil
		mu.Unlock()
	})
}

func waitForIdleWorker(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		opMu.Lock()
		working := opWorking
		opMu.Unlock()
		if !working {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("lifecycle worker did not finish")
}

func waitForCalls(t *testing.T, rec *opRecorder, want []string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var got []string
	for time.Now().Before(deadline) {
		got = rec.snapshot()
		if len(got) >= len(want) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if len(got) != len(want) {
		t.Fatalf("expected calls %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected calls %v, got %v", want, got)
		}
	}
}

func TestRequestStartRunsOnceAndSettles(t *testing.T) {
	rec := &opRecorder{}
	installOpRecorder(t, rec)

	RequestStart()
	waitForIdleWorker(t)
	waitForCalls(t, rec, []string{"start"})

	if State().Phase != PhaseStarting {
		t.Fatalf("expected the phase to stay at starting until health confirms, got %q", State().Phase)
	}
}

func TestRapidToggleEndsInTheLastRequestedState(t *testing.T) {
	rec := &opRecorder{block: make(chan struct{})}
	installOpRecorder(t, rec)

	RequestStart()
	waitForCalls(t, rec, []string{"start"})

	RequestStop()
	RequestStart()
	RequestStop()

	close(rec.block)
	rec.mu.Lock()
	rec.block = nil
	rec.mu.Unlock()

	waitForIdleWorker(t)

	calls := rec.snapshot()
	if len(calls) < 2 || calls[len(calls)-2] != "stop" || calls[len(calls)-1] != "restore" {
		t.Fatalf("expected the final operations to be stop then restore, got %v", calls)
	}
	if State().Phase != PhaseIdle {
		t.Fatalf("expected idle phase after a final stop, got %q", State().Phase)
	}
}

func TestStopReportsIdleBeforeRestoringSystemSettings(t *testing.T) {
	rec := &opRecorder{}
	installOpRecorder(t, rec)

	phaseAtRestore := make(chan Phase, 1)
	restoreFn = func() {
		phaseAtRestore <- State().Phase
		rec.record("restore")
	}

	RequestStop()
	waitForIdleWorker(t)

	if got := <-phaseAtRestore; got != PhaseIdle {
		t.Fatalf("the UI must see the bypass as off before DNS/tuning cleanup finishes, got %q", got)
	}
	waitForCalls(t, rec, []string{"stop", "restore"})
}

func TestStopFollowedByStartSkipsTheRestoreChurn(t *testing.T) {
	rec := &opRecorder{}
	installOpRecorder(t, rec)

	haltFn = func() {
		rec.record("stop")
		opMu.Lock()
		opDesired = true
		opMu.Unlock()
	}

	RequestStop()
	waitForIdleWorker(t)

	calls := rec.snapshot()
	for _, call := range calls {
		if call == "restore" {
			t.Fatalf("a stop that is immediately undone must not flip DNS/tuning back and forth, got %v", calls)
		}
	}
	if calls[len(calls)-1] != "start" {
		t.Fatalf("expected the pending start to run, got %v", calls)
	}
}

func TestRequestRestartAlwaysStopsBeforeStarting(t *testing.T) {
	rec := &opRecorder{}
	installOpRecorder(t, rec)

	RequestRestart()
	waitForIdleWorker(t)

	waitForCalls(t, rec, []string{"stop", "start"})
}

func TestFailedStartReportsErrorAndReturnsToIdle(t *testing.T) {
	rec := &opRecorder{err: errors.New("servis baslatilamadi")}
	installOpRecorder(t, rec)

	errs := make(chan string, 1)
	SetErrorCallback(func(message string) { errs <- message })

	RequestStart()
	waitForIdleWorker(t)

	select {
	case msg := <-errs:
		if msg != "servis baslatilamadi" {
			t.Fatalf("unexpected error surfaced: %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a failed start must surface an error to the UI")
	}

	if State().Phase != PhaseIdle {
		t.Fatalf("expected idle phase after a failed start, got %q", State().Phase)
	}
	if State().Active {
		t.Fatal("a failed start must not report the service as active")
	}
}
