package engine

import (
	"errors"
	"sync"
	"time"
)

type Phase string

const (
	PhaseIdle       Phase = "idle"
	PhaseStarting   Phase = "starting"
	PhaseRunning    Phase = "running"
	PhaseRecovering Phase = "recovering"
	PhaseStopping   Phase = "stopping"
)

type Status struct {
	Phase   Phase  `json:"phase"`
	Active  bool   `json:"active"`
	Healthy bool   `json:"healthy"`
	Profile string `json:"profile"`
	Detail  string `json:"detail"`
}

var (
	opMu      sync.Mutex
	opDesired bool
	opRestart bool
	opWorking bool
	opClosed  bool

	startFn   = Start
	haltFn    = haltService
	restoreFn = restoreSystemOptimizations
)

func RequestStart() { requestOp(true, false) }

func RequestStop() { requestOp(false, false) }

func RequestRestart() { requestOp(true, true) }

func requestOp(start bool, restart bool) {
	opMu.Lock()
	if opClosed && start {
		opMu.Unlock()
		return
	}
	changed := opDesired != start || restart
	opDesired = start
	if restart {
		opRestart = true
	}
	if opWorking {
		opMu.Unlock()
		if changed {
			cancelScans()
		}
		return
	}
	opWorking = true
	opMu.Unlock()

	announceOp(start)
	go opWorker()
}

func opWorker() {
	for {
		opMu.Lock()
		want := opDesired
		restart := opRestart
		opRestart = false
		opMu.Unlock()

		settled := true
		if want {
			if restart {
				setPhase(PhaseStopping, "mod degistiriliyor")
				haltFn()
			}
			if err := startFn(); errors.Is(err, errScanCanceled) {
				settled = false
			} else if err != nil {
				setPhase(PhaseIdle, "")
				notifyError(err.Error())
			}
		} else {
			setDNSWanted(false)
			setTuningWanted(false)
			haltFn()
			setPhase(PhaseIdle, "")

			opMu.Lock()
			startPending := opDesired
			opMu.Unlock()
			if !startPending {
				restoreFn()
			}
		}

		opMu.Lock()
		if settled && opDesired == want && !opRestart {
			opWorking = false
			opMu.Unlock()
			return
		}
		next := opDesired
		opMu.Unlock()

		announceOp(next)
	}
}

func Shutdown(timeout time.Duration) bool {
	opMu.Lock()
	opClosed = true
	opMu.Unlock()

	RequestStop()

	deadline := time.Now().Add(timeout)
	for {
		opMu.Lock()
		working := opWorking
		opMu.Unlock()
		if !working {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func announceOp(start bool) {
	if start {
		setPhase(PhaseStarting, "baglanti hazirlaniyor")
		return
	}
	setPhase(PhaseStopping, "baglanti kapatiliyor")
}

func State() Status {
	mu.Lock()
	defer mu.Unlock()
	return statusLocked()
}

func statusLocked() Status {
	return Status{
		Phase:   currentPhase,
		Active:  isActivePhase(currentPhase),
		Healthy: connectionHealthy && isProcessAlive(winwsProcess),
		Profile: activeStrategy,
		Detail:  phaseDetail,
	}
}

func isActivePhase(phase Phase) bool {
	return phase == PhaseStarting || phase == PhaseRunning || phase == PhaseRecovering
}

func setPhase(phase Phase, detail string) {
	mu.Lock()
	if currentPhase == phase && phaseDetail == detail {
		mu.Unlock()
		return
	}
	currentPhase = phase
	phaseDetail = detail
	status := statusLocked()
	cb := statusCallback
	mu.Unlock()

	if cb != nil {
		cb(status)
	}
}

func notifyError(message string) {
	mu.Lock()
	cb := errorCallback
	mu.Unlock()

	if cb != nil {
		cb(message)
	}
}
