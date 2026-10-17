package glide

import (
	"context"
	"crypto/tls"
	"net"
	"sync/atomic"
	"time"
)

const (
	verifyHost    = "discord.com"
	verifyTimeout = 6 * time.Second
	verifySettle  = 2 * time.Second
)

type desyncConn struct {
	net.Conn
	strategy strategy
	first    atomic.Bool
}

func (c *desyncConn) Write(b []byte) (int, error) {
	if c.first.CompareAndSwap(false, true) {
		if err := writeDesynced(c.Conn, b, c.strategy); err != nil {
			if _, werr := c.Conn.Write(b); werr != nil {
				return 0, werr
			}
		}
		return len(b), nil
	}
	return c.Conn.Write(b)
}

func (t *Tunnel) verify() {
	var ips []net.IP
	for attempt := 0; attempt < 4; attempt++ {
		if t.stopped.Load() {
			return
		}
		time.Sleep(verifySettle)
		ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout*2)
		resolved, err := t.resolver.resolveA(ctx, verifyHost)
		cancel()
		if err == nil && len(resolved) > 0 {
			ips = resolved
			break
		}
	}
	if len(ips) == 0 {
		t.logf("dogrulama: dns yaniti yok")
		return
	}
	for _, s := range strategyOrder(t.strategyID()) {
		if t.stopped.Load() {
			return
		}
		if t.probe(ips, s) {
			t.setStrategyID(s.ID)
			t.logf("strateji secildi: %s", s.ID)
			return
		}
	}
	t.logf("dogrulama: uygun strateji bulunamadi")
}

func (t *Tunnel) probe(ips []net.IP, s strategy) bool {
	raw := t.dial(ips, 443)
	if raw == nil {
		return false
	}
	defer raw.Close()
	conn := &desyncConn{Conn: raw, strategy: s}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: verifyHost})
	deadline := time.Now().Add(verifyTimeout)
	raw.SetDeadline(deadline)
	hctx, hcancel := context.WithDeadline(context.Background(), deadline)
	defer hcancel()
	err := tlsConn.HandshakeContext(hctx)
	raw.SetDeadline(time.Time{})
	if err != nil {
		return false
	}
	tlsConn.Close()
	return true
}
