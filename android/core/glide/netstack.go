package glide

import (
	"errors"
	"net/netip"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	nicID          = 1
	tcpRcvWnd      = 0
	tcpMaxInFlight = 512
)

type tunStack struct {
	stack *stack.Stack
}

func startTunStack(fd int, opts *Options, t *Tunnel) (*tunStack, error) {
	ep, err := fdbased.New(&fdbased.Options{
		FDs:            []int{fd},
		MTU:            uint32(opts.Mtu),
		EthernetHeader: false,
	})
	if err != nil {
		return nil, err
	}
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	if tcpErr := s.CreateNIC(nicID, ep); tcpErr != nil {
		return nil, errors.New(tcpErr.String())
	}
	s.SetPromiscuousMode(nicID, true)
	s.SetSpoofing(nicID, true)
	s.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: nicID},
		{Destination: header.IPv6EmptySubnet, NIC: nicID},
	})

	tcpFwd := tcp.NewForwarder(s, tcpRcvWnd, tcpMaxInFlight, func(r *tcp.ForwarderRequest) {
		t.handleTCP(r)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)

	udpFwd := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		return t.handleUDP(r)
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpFwd.HandlePacket)

	return &tunStack{stack: s}, nil
}

func (ts *tunStack) close() {
	ts.stack.Close()
}

func (t *Tunnel) isDNS(addr netip.Addr) bool {
	return addr == t.dnsV4 || addr == t.dnsV6
}

func (t *Tunnel) handleTCP(r *tcp.ForwarderRequest) {
	id := r.ID()
	dst, ok := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	if !ok {
		r.Complete(true)
		return
	}
	dst = dst.Unmap()
	port := int(id.LocalPort)

	if t.isDNS(dst) && port != 53 {
		r.Complete(true)
		return
	}

	var wq waiter.Queue
	epx, tcpErr := r.CreateEndpoint(&wq)
	if tcpErr != nil {
		r.Complete(true)
		return
	}
	r.Complete(false)
	local := gonet.NewTCPConn(&wq, epx)
	t.served.Add(1)

	if t.isDNS(dst) {
		go t.serveDNSTCP(local)
		return
	}
	go t.pipeTCP(local, dst, port)
}

func (t *Tunnel) handleUDP(r *udp.ForwarderRequest) bool {
	id := r.ID()
	dst, ok := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	if !ok {
		return true
	}
	dst = dst.Unmap()
	port := int(id.LocalPort)

	var wq waiter.Queue
	epx, tcpErr := r.CreateEndpoint(&wq)
	if tcpErr != nil {
		return true
	}
	conn := gonet.NewUDPConn(&wq, epx)

	if t.isDNS(dst) && port == 53 {
		go t.serveDNSUDP(conn)
		return true
	}
	go t.relayUDP(conn, netip.AddrPortFrom(dst, uint16(port)))
	return true
}
