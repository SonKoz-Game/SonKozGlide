package engine

import (
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type netInterface struct {
	Index          int
	Alias          string
	GUID           string
	MTU            int
	HasIPv4Gateway bool
	DNSServers     []string
}

var procGetIpInterfaceEntry = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIpInterfaceEntry")

func listGatewayInterfaces() ([]netInterface, error) {
	const flags = windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST

	size := uint32(16 * 1024)
	for attempt := 0; attempt < 4; attempt++ {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return collectGatewayInterfaces(first), nil
	}
	return nil, errors.New("adapter list changed while it was being read")
}

func collectGatewayInterfaces(first *windows.IpAdapterAddresses) []netInterface {
	var ifaces []netInterface
	for aa := first; aa != nil; aa = aa.Next {
		if aa.OperStatus != windows.IfOperStatusUp || aa.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}

		hasV4, hasV6 := false, false
		for gw := aa.FirstGatewayAddress; gw != nil; gw = gw.Next {
			ip := gw.Address.IP()
			if ip == nil || ip.IsUnspecified() {
				continue
			}
			if ip.To4() != nil {
				hasV4 = true
			} else {
				hasV6 = true
			}
		}
		if !hasV4 && !hasV6 {
			continue
		}

		index := aa.IfIndex
		if index == 0 {
			index = aa.Ipv6IfIndex
		}
		if index == 0 {
			continue
		}

		iface := netInterface{
			Index:          int(index),
			Alias:          windows.UTF16PtrToString(aa.FriendlyName),
			GUID:           windows.BytePtrToString(aa.AdapterName),
			MTU:            ipv4InterfaceMTU(aa.IfIndex, aa.Mtu),
			HasIPv4Gateway: hasV4,
		}
		for dns := aa.FirstDnsServerAddress; dns != nil; dns = dns.Next {
			if ip := dns.Address.IP(); ip != nil {
				iface.DNSServers = append(iface.DNSServers, ip.String())
			}
		}
		ifaces = append(ifaces, iface)
	}

	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Index < ifaces[j].Index })
	return ifaces
}

func ipv4InterfaceMTU(index uint32, fallback uint32) int {
	if index == 0 || procGetIpInterfaceEntry.Find() != nil {
		return int(fallback)
	}

	row := windows.MibIpInterfaceRow{Family: windows.AF_INET, InterfaceIndex: index}
	if ret, _, _ := procGetIpInterfaceEntry.Call(uintptr(unsafe.Pointer(&row))); ret != 0 || row.NlMtu == 0 {
		return int(fallback)
	}
	return int(row.NlMtu)
}

func interfaceSignature(ifaces []netInterface) string {
	parts := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		parts = append(parts, strconv.Itoa(iface.Index))
	}
	return strings.Join(parts, ",")
}

func staticNameServers(guid string) (string, string) {
	if guid == "" {
		return "", ""
	}
	v4 := readNameServer(`SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\` + guid)
	v6 := readNameServer(`SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters\Interfaces\` + guid)
	return v4, v6
}

func readNameServer(path string) string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	value, _, err := key.GetStringValue("NameServer")
	if err != nil {
		return ""
	}
	return normalizeServerList(value)
}

func normalizeServerList(raw string) string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	return strings.Join(fields, ",")
}

func serversOfFamily(servers []string, v6 bool) string {
	picked := make([]string, 0, len(servers))
	for _, server := range servers {
		ip := net.ParseIP(server)
		if ip == nil || (ip.To4() == nil) != v6 {
			continue
		}
		picked = append(picked, server)
	}
	return strings.Join(picked, ",")
}
