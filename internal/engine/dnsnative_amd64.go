package engine

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const nativeDNSSupported = true

func guidArgs(id *windows.GUID) []uintptr {
	return []uintptr{uintptr(unsafe.Pointer(id))}
}
