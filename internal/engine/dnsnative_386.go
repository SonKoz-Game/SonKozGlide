package engine

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const nativeDNSSupported = true

func guidArgs(id *windows.GUID) []uintptr {
	words := (*[4]uint32)(unsafe.Pointer(id))
	return []uintptr{uintptr(words[0]), uintptr(words[1]), uintptr(words[2]), uintptr(words[3])}
}
