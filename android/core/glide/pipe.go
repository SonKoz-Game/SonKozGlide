package glide

import (
	"context"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	dialTimeout   = 8 * time.Second
	firstReadWait = 3 * time.Second
	idleTimeout   = 120 * time.Second
	maxFirst      = 16 * 1024
)

func (t *Tunnel) pipeTCP(local net.Conn, name string, port int) {
	defer local.Close()

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	ips, err := t.resolver.resolveA(ctx, name)
	cancel()
	if err != nil || len(ips) == 0 {
		return
	}

	remote := t.dial(ips, port)
	if remote == nil {
		return
	}
	defer remote.Close()

	if port == 443 {
		t.desyncHandshake(local, remote)
	}

	pump(local, remote)
}

func (t *Tunnel) dial(ips []net.IP, port int) net.Conn {
	dialer := &net.Dialer{Timeout: dialTimeout}
	for _, ip := range ips {
		addr := net.JoinHostPort(ip.String(), strconv.Itoa(port))
		conn, err := dialer.Dial("tcp", addr)
		if err == nil {
			if tcp, ok := conn.(*net.TCPConn); ok {
				tcp.SetNoDelay(true)
			}
			return conn
		}
	}
	return nil
}

func (t *Tunnel) desyncHandshake(local, remote net.Conn) {
	local.SetReadDeadline(time.Now().Add(firstReadWait))
	first := make([]byte, maxFirst)
	n, err := local.Read(first)
	local.SetReadDeadline(time.Time{})
	if n <= 0 {
		if err == nil {
			return
		}
		return
	}
	first = first[:n]

	s, ok := strategyByID(t.strategyID())
	if !ok {
		remote.Write(first)
		return
	}
	if werr := writeDesynced(remote, first, s); werr != nil {
		remote.Write(first)
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
