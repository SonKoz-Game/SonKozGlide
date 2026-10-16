package main

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SonKoz-Game/SonKozGlide/internal/engine"
	"github.com/SonKoz-Game/SonKozGlide/internal/settings"
)

func connectedPair() (*ipcConn, *ipcConn) {
	uiRead, coreWrite := io.Pipe()
	coreRead, uiWrite := io.Pipe()
	return newIPCConn(uiRead, uiWrite), newIPCConn(coreRead, coreWrite)
}

type fakeCore struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeCore) Echo(cfg settings.Config, enable bool) settings.Config {
	f.record("Echo")
	cfg.SafeDNS = enable
	return cfg
}

func (f *fakeCore) Status() engine.Status {
	f.record("Status")
	return engine.Status{Phase: engine.PhaseRunning, Detail: "dogrulama suruyor"}
}

func (f *fakeCore) Nothing() {
	f.record("Nothing")
}

func (f *fakeCore) record(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
}

func serveTarget(conn *ipcConn, target any, events chan<- ipcMessage) {
	conn.serve(func(msg ipcMessage) {
		if msg.Method == "" {
			if events != nil {
				events <- msg
			}
			return
		}
		go func() {
			result, err := invokeMethod(target, msg.Method, msg.Params)
			conn.reply(msg.ID, result, err)
		}()
	})
}

func TestIPCCallRoundTripsTypedValues(t *testing.T) {
	ui, core := connectedPair()
	go serveTarget(core, &fakeCore{}, nil)
	go ui.serve(func(ipcMessage) {})

	var cfg settings.Config
	if err := ui.call("Echo", &cfg, settings.Config{ISPProfile: "turktelekom", AutoUpdate: true}, true); err != nil {
		t.Fatalf("call: %v", err)
	}
	if cfg.ISPProfile != "turktelekom" || !cfg.AutoUpdate || !cfg.SafeDNS {
		t.Fatalf("round trip lost fields: %+v", cfg)
	}

	var status engine.Status
	if err := ui.call("Status", &status); err != nil || status.Phase != engine.PhaseRunning {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if err := ui.call("Nothing", nil); err != nil {
		t.Fatalf("void call: %v", err)
	}
	if err := ui.call("Missing", nil); err == nil {
		t.Fatal("unknown method did not fail")
	}
	if err := ui.call("Echo", &cfg, "not a config", true); err == nil {
		t.Fatal("bad argument did not fail")
	}
}

func TestIPCConcurrentCallsGetTheirOwnReplies(t *testing.T) {
	ui, core := connectedPair()
	go serveTarget(core, &fakeCore{}, nil)
	go ui.serve(func(ipcMessage) {})

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			profile := string(rune('a' + i%26))
			var cfg settings.Config
			if err := ui.call("Echo", &cfg, settings.Config{ISPProfile: profile}, i%2 == 0); err != nil {
				errs <- err
				return
			}
			if cfg.ISPProfile != profile || cfg.SafeDNS != (i%2 == 0) {
				errs <- errors.New("reply belongs to another call")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestIPCEventsReachTheOtherSide(t *testing.T) {
	ui, core := connectedPair()
	events := make(chan ipcMessage, 4)
	go serveTarget(ui, &fakeCore{}, events)
	go core.serve(func(ipcMessage) {})

	core.notify("service_state", engine.Status{Phase: engine.PhaseStarting})
	select {
	case msg := <-events:
		var status engine.Status
		if msg.Event != "service_state" || json.Unmarshal(msg.Data, &status) != nil || status.Phase != engine.PhaseStarting {
			t.Fatalf("unexpected event %+v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event never arrived")
	}
}

func TestIPCCloseFailsPendingCallsAndFlushesQueuedEvents(t *testing.T) {
	ui, core := connectedPair()
	events := make(chan ipcMessage, 4)
	done := make(chan struct{})
	go func() {
		serveTarget(ui, &fakeCore{}, events)
		close(done)
	}()

	core.notify(uiQuitEvent, nil)
	core.close()

	select {
	case msg := <-events:
		if msg.Event != uiQuitEvent {
			t.Fatalf("got %+v, want the quit event", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued event was dropped on close")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the other side never saw the connection close")
	}
	if err := ui.call("Status", nil); !errors.Is(err, errIPCClosed) {
		t.Fatalf("call after close = %v, want errIPCClosed", err)
	}
}

func TestIPCPendingCallFailsWhenThePeerDies(t *testing.T) {
	ui, core := connectedPair()
	go ui.serve(func(ipcMessage) {})
	go core.serve(func(ipcMessage) {})

	result := make(chan error, 1)
	go func() {
		result <- ui.call("Status", nil)
	}()
	time.Sleep(50 * time.Millisecond)
	core.close()

	select {
	case err := <-result:
		if !errors.Is(err, errIPCClosed) {
			t.Fatalf("got %v, want errIPCClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a call to a dead peer hung")
	}
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

func TestEveryBoundMethodHasAMatchingCoreMethod(t *testing.T) {
	appType := reflect.TypeOf(&App{})
	coreType := reflect.TypeOf(&Core{})

	for i := 0; i < appType.NumMethod(); i++ {
		method := appType.Method(i)
		target, ok := coreType.MethodByName(method.Name)
		if !ok {
			t.Errorf("App.%s has no Core.%s to call", method.Name, method.Name)
			continue
		}

		bound, impl := method.Type, target.Type
		if bound.NumIn() != impl.NumIn() {
			t.Errorf("%s: App takes %d arguments, Core takes %d", method.Name, bound.NumIn()-1, impl.NumIn()-1)
			continue
		}
		for j := 1; j < bound.NumIn(); j++ {
			if bound.In(j) != impl.In(j) {
				t.Errorf("%s: argument %d is %s in App but %s in Core", method.Name, j, bound.In(j), impl.In(j))
			}
		}

		if bound.NumOut() == 0 || bound.Out(bound.NumOut()-1) != errorType {
			t.Errorf("App.%s must return an error so JS sees a dead core as a rejected promise", method.Name)
			continue
		}
		if bound.NumOut()-1 != impl.NumOut() {
			t.Errorf("%s: App returns %d values, Core returns %d", method.Name, bound.NumOut()-1, impl.NumOut())
			continue
		}
		if impl.NumOut() == 1 && bound.Out(0) != impl.Out(0) {
			t.Errorf("%s: App returns %s, Core returns %s", method.Name, bound.Out(0), impl.Out(0))
		}
	}
}

func TestEveryBoundMethodCallsTheCoreMethodOfTheSameName(t *testing.T) {
	ui, core := connectedPair()
	go ui.serve(func(ipcMessage) {})

	called := make(chan string, 1)
	go core.serve(func(msg ipcMessage) {
		called <- msg.Method
		core.reply(msg.ID, nil, nil)
	})

	app := NewApp(ui)
	value := reflect.ValueOf(app)
	appType := value.Type()
	for i := 0; i < appType.NumMethod(); i++ {
		method := appType.Method(i)
		args := make([]reflect.Value, method.Type.NumIn()-1)
		for j := range args {
			args[j] = reflect.Zero(method.Type.In(j + 1))
		}
		value.Method(i).Call(args)

		select {
		case name := <-called:
			if name != method.Name {
				t.Errorf("App.%s asked the core for %q", method.Name, name)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("App.%s never reached the core", method.Name)
		}
	}
}
