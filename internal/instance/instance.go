package instance

import (
	"time"

	"golang.org/x/sys/windows"
)

const pollInterval = 100 * time.Millisecond

func Acquire(name string, wait time.Duration) (windows.Handle, bool) {
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}

	deadline := time.Now().Add(wait)
	for {
		handle, err := windows.CreateMutex(nil, true, ptr)
		if err != windows.ERROR_ALREADY_EXISTS {
			return handle, true
		}
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		if !time.Now().Before(deadline) {
			return 0, false
		}
		time.Sleep(pollInterval)
	}
}
