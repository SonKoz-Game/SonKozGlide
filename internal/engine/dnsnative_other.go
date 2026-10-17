//go:build !amd64 && !386

package engine

import "golang.org/x/sys/windows"

const nativeDNSSupported = false

func guidArgs(*windows.GUID) []uintptr {
	return nil
}
