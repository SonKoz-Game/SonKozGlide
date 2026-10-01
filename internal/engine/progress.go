package engine

import (
	"sync"
	"time"
)

const (
	stepActive  = "active"
	stepDone    = "done"
	stepFailed  = "failed"
	stepSkipped = "skipped"

	maxProgressEvents = 96
)

type ProgressEvent struct {
	Run     uint64 `json:"run"`
	Seq     int    `json:"seq"`
	Step    string `json:"step"`
	State   string `json:"state"`
	Detail  string `json:"detail,omitempty"`
	Index   int    `json:"index,omitempty"`
	Total   int    `json:"total,omitempty"`
	OK      int    `json:"ok,omitempty"`
	Count   int    `json:"count,omitempty"`
	Latency int64  `json:"latency,omitempty"`
	At      int64  `json:"at"`
}

var (
	progressMu       sync.Mutex
	progressRun      uint64
	progressStarted  time.Time
	progressLog      []ProgressEvent
	progressCallback func(event ProgressEvent)
)

func SetProgressCallback(cb func(event ProgressEvent)) {
	progressMu.Lock()
	progressCallback = cb
	progressMu.Unlock()
}

func beginProgressRun(kind string) uint64 {
	progressMu.Lock()
	progressRun++
	run := progressRun
	progressStarted = time.Now()
	progressLog = nil
	progressMu.Unlock()

	emitProgress(ProgressEvent{Run: run, Step: "run", State: stepActive, Detail: kind})
	return run
}

func emitProgress(event ProgressEvent) {
	progressMu.Lock()
	if event.Run == 0 || event.Run != progressRun {
		progressMu.Unlock()
		return
	}
	event.Seq = len(progressLog) + 1
	event.At = time.Since(progressStarted).Milliseconds()
	if len(progressLog) < maxProgressEvents {
		progressLog = append(progressLog, event)
	}
	cb := progressCallback
	progressMu.Unlock()

	if cb != nil {
		cb(event)
	}
}

func step(run uint64, name string, state string, detail string) {
	emitProgress(ProgressEvent{Run: run, Step: name, State: state, Detail: detail})
}

func ConnectTrace() []ProgressEvent {
	progressMu.Lock()
	defer progressMu.Unlock()

	out := make([]ProgressEvent, len(progressLog))
	copy(out, progressLog)
	return out
}
