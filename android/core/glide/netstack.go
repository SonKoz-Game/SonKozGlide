package glide

import (
	"context"
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

	dnsV4, _ := netip.ParseAddr(opts.DnsFakeV4)
	dnsV6, _ := netip.ParseAddr(opts.DnsFakeV6)

	tcpFwd := tcp.NewForwarder(s, tcpRcvWnd, tcpMaxInFlight, func(r *tcp.ForwarderRequest) {
		t.handleTCP(s, r)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)

	udpFwd := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		return t.handleUDP(s, r, dnsV4, dnsV6)
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpFwd.HandlePacket)

	return &tunStack{stack: s}, nil
}

func (ts *tunStack) close() {
	ts.stack.Close()
}

func (t *Tunnel) handleTCP(s *stack.Stack, r *tcp.ForwarderRequest) {
	id := r.ID()
	fakeAddr, ok := netip.AddrFromSlice([]byte(id.LocalAddress.AsSlice()))
	if !ok {
		r.Complete(true)
		return
	}
	fakeAddr = fakeAddr.Unmap()
	name, found := t.fakePool.lookup(fakeAddr)
	if !found {
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
	go t.pipeTCP(local, name, int(id.LocalPort))
}

func (t *Tunnel) handleUDP(s *stack.Stack, r *udp.ForwarderRequest, dnsV4, dnsV6 netip.Addr) bool {
	id := r.ID()
	dst, ok := netip.AddrFromSlice([]byte(id.LocalAddress.AsSlice()))
	if !ok {
		return true
	}
	dst = dst.Unmap()
	if id.LocalPort != 53 || (dst != dnsV4 && dst != dnsV6) {
		return true
	}
	var wq waiter.Queue
	epx, tcpErr := r.CreateEndpoint(&wq)
	if tcpErr != nil {
		return true
	}
	conn := gonet.NewUDPConn(&wq, epx)
	go t.serveDNSConn(conn)
	return true
}

func (t *Tunnel) serveDNSConn(conn *gonet.UDPConn) {
	defer conn.Close()
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	reply := t.handleDNS(context.Background(), buf[:n])
	if reply != nil {
		conn.Write(reply)
	}
}
