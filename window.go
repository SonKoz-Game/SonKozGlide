package main

import (
	"errors"
	"os"
	"runtime"
	"runtime/debug"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	asfwAny                  = -1
	eventSystemMinimizeStart = 0x0016
	eventSystemMinimizeEnd   = 0x0017
	winEventOutOfContext     = 0x0000
	objidWindow              = 0
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procSetWinEventHook          = user32.NewProc("SetWinEventHook")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procSetProcessWorkingSetSize = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetProcessWorkingSetSize")

	errNoCorePipe = errors.New("ui: not started by the Glide core")
)

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      [2]int32
	private uint32
}

func allowForeground(pid int) {
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(uint32(pid)))
}

func trimWorkingSet() {
	debug.FreeOSMemory()
	_, _, _ = procSetProcessWorkingSetSize.Call(uintptr(windows.CurrentProcess()), ^uintptr(0), ^uintptr(0))
}

func watchMinimize(onChange func(minimized bool)) {
	runtime.LockOSThread()

	callback := windows.NewCallback(func(hook, event, hwnd, idObject, idChild, thread, at uintptr) uintptr {
		if int32(idObject) == objidWindow {
			onChange(event == eventSystemMinimizeStart)
		}
		return 0
	})
	hook, _, _ := procSetWinEventHook.Call(
		eventSystemMinimizeStart,
		eventSystemMinimizeEnd,
		0,
		callback,
		uintptr(os.Getpid()),
		0,
		winEventOutOfContext,
	)
	if hook == 0 {
		return
	}

	var msg winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
	}
}

func attachCore() (*ipcConn, error) {
	nul, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	in, err := takeStdHandle(os.Stdin, windows.STD_INPUT_HANDLE, nul)
	if err != nil {
		return nil, err
	}
	os.Stdin = nul
	out, err := takeStdHandle(os.Stdout, windows.STD_OUTPUT_HANDLE, nul)
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	os.Stdout = nul
	return newIPCConn(in, out), nil
}

func takeStdHandle(std *os.File, id uint32, nul *os.File) (*os.File, error) {
	if std == nil {
		return nil, errNoCorePipe
	}
	handle := windows.Handle(std.Fd())
	if kind, err := windows.GetFileType(handle); err != nil || kind != windows.FILE_TYPE_PIPE {
		return nil, errNoCorePipe
	}

	self := windows.CurrentProcess()
	var private windows.Handle
	if err := windows.DuplicateHandle(self, handle, self, &private, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	_ = windows.SetStdHandle(id, windows.Handle(nul.Fd()))
	_ = std.Close()
	return os.NewFile(uintptr(private), "glide-core"), nil
}

func createShowEvent() (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(showEventName)
	if err != nil {
		return 0, err
	}
	return windows.CreateEvent(nil, 0, 0, name)
}

func signalRunningInstance() {
	allowForeground(asfwAny)

	name, err := windows.UTF16PtrFromString(showEventName)
	if err != nil {
		return
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return
	}
	defer windows.CloseHandle(event)
	_ = windows.SetEvent(event)
}
