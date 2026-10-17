package glide

import (
	"io"
	"net"
	"net/netip"
	"strconv"
	"time"
)

const (
	dialTimeout   = 8 * time.Second
	firstReadWait = 2 * time.Second
	idleTimeout   = 120 * time.Second
	udpIdle       = 60 * time.Second
	maxFirst      = 16 * 1024
)

func (t *Tunnel) pipeTCP(local net.Conn, dst netip.Addr, port int) {
	defer local.Close()

	remote := t.dialAddr(dst, port)
	if remote == nil {
		return
	}
	defer remote.Close()

	if port == 443 {
		t.startHTTPS(local, remote)
	}
	pump(local, remote)
}

func (t *Tunnel) startHTTPS(local, remote net.Conn) {
	local.SetReadDeadline(time.Now().Add(firstReadWait))
	first := make([]byte, maxFirst)
	n, _ := local.Read(first)
	local.SetReadDeadline(time.Time{})
	if n <= 0 {
		return
	}
	first = first[:n]

	host, _ := sniHost(first)
	if host != "" && t.rules.match(host) {
		s, ok := strategyByID(t.strategyID())
		if ok {
			if werr := writeDesynced(remote, first, s); werr == nil {
				return
			}
		}
	}
	remote.Write(first)
}

func (t *Tunnel) dialAddr(dst netip.Addr, port int) net.Conn {
	dialer := &net.Dialer{Timeout: dialTimeout}
	addr := net.JoinHostPort(dst.String(), strconv.Itoa(port))
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
	}
	return conn
}

func (t *Tunnel) dial(ips []net.IP, port int) net.Conn {
	dialer := &net.Dialer{Timeout: dialTimeout}
	for _, ip := range ips {
		conn, err := dialer.Dial("tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		if err == nil {
			if tcp, ok := conn.(*net.TCPConn); ok {
				tcp.SetNoDelay(true)
			}
			return conn
		}
	}
	return nil
}

func (t *Tunnel) relayUDP(local net.Conn, dst netip.AddrPort) {
	defer local.Close()
	remote, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(dst))
	if err != nil {
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() { copyUDP(remote, local); done <- struct{}{} }()
	go func() { copyUDP(local, remote); done <- struct{}{} }()
	<-done
	local.Close()
	remote.Close()
	<-done
}

func copyUDP(dst, src net.Conn) {
	buf := make([]byte, 64*1024)
	for {
		src.SetReadDeadline(time.Now().Add(udpIdle))
		n, err := src.Read(buf)
		if n > 0 {
			dst.SetWriteDeadline(time.Now().Add(udpIdle))
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func pump(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		copyIdle(b, a)
		done <- struct{}{}
	}()
	go func() {
		copyIdle(a, b)
		done <- struct{}{}
	}()
	<-done
	a.Close()
	b.Close()
	<-done
}

func copyIdle(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		src.SetReadDeadline(time.Now().Add(idleTimeout))
		n, err := src.Read(buf)
		if n > 0 {
			dst.SetWriteDeadline(time.Now().Add(idleTimeout))
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				if tcp, ok := dst.(*net.TCPConn); ok {
					tcp.CloseWrite()
				}
			}
			return
		}
	}
}
