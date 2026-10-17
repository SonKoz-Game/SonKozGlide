package engine

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	dnsInterfaceSettingsVersion1 = 1
	dnsSettingIPv6               = 0x0001
	dnsSettingNameServer         = 0x0002
)

type dnsInterfaceSettings struct {
	Version             uint32
	_                   uint32
	FlagsLow            uint32
	FlagsHigh           uint32
	Domain              *uint16
	NameServer          *uint16
	SearchList          *uint16
	RegistrationEnabled uint32
	RegisterAdapterName uint32
	EnableLLMNR         uint32
	QueryAdapterName    uint32
	ProfileNameServer   *uint16
}

var (
	procSetInterfaceDnsSettings = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("SetInterfaceDnsSettings")

	errNativeDNSUnavailable = errors.New("SetInterfaceDnsSettings is not available")
)

func nativeDNSAvailable() bool {
	return nativeDNSSupported && procSetInterfaceDnsSettings.Find() == nil
}

func setInterfaceNameServers(guid string, servers string, v6 bool) error {
	if !nativeDNSAvailable() {
		return errNativeDNSUnavailable
	}
	id, err := windows.GUIDFromString(guid)
	if err != nil {
		return err
	}
	list, err := windows.UTF16PtrFromString(servers)
	if err != nil {
		return err
	}

	flags := uint32(dnsSettingNameServer)
	if v6 {
		flags |= dnsSettingIPv6
	}
	settings := dnsInterfaceSettings{
		Version:    dnsInterfaceSettingsVersion1,
		FlagsLow:   flags,
		NameServer: list,
	}
	args := append(guidArgs(&id), uintptr(unsafe.Pointer(&settings)))
	ret, _, _ := syscall.SyscallN(procSetInterfaceDnsSettings.Addr(), args...)
	if ret != 0 {
		return syscall.Errno(ret)
	}
	return nil
}

func applyDNSServersNative(iface netInterface) error {
	if err := setInterfaceNameServers(iface.GUID, dnsServerCSV(false), false); err != nil {
		return err
	}
	if err := setInterfaceNameServers(iface.GUID, v6ServerCSV(), true); err != nil {
		appendEngineLog(fmt.Sprintf("dns: ipv6 servers not set on %d: %v", iface.Index, err))
	}
	return nil
}

func restoreDNSServersNative(iface dnsInterfaceBackup) error {
	servers4, servers6 := "", ""
	if iface.Mode == "static" {
		servers4 = nativeServerList(iface.Servers)
	}
	if iface.Mode6 == "static" {
		servers6 = nativeServerList(iface.Servers6)
	}
	if err := setInterfaceNameServers(iface.GUID, servers4, false); err != nil {
		return err
	}
	return setInterfaceNameServers(iface.GUID, servers6, true)
}

func v6ServerCSV() string {
	ips := make([]string, 0, len(encryptedDNSServersV6))
	for _, server := range encryptedDNSServersV6 {
		ips = append(ips, server.IP)
	}
	return strings.Join(ips, ",")
}

func nativeServerList(csv string) string {
	parts := strings.Split(csv, ",")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		ip := strings.TrimSpace(part)
		if ip != "" && isPlausibleIP(ip) {
			kept = append(kept, ip)
		}
	}
	return strings.Join(kept, ",")
}
