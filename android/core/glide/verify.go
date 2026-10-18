package glide

import (
	"context"
	"crypto/tls"
	"net"
	"sync/atomic"
	"time"
)

const (
	verifyHost     = "discord.com"
	verifyTimeout  = 6 * time.Second
	verifySettle   = 2 * time.Second
	verifyProbes   = 6
	verifyAccept   = 4
	verifyKeep     = 2
	verifyRetryGap = 45 * time.Second
	verifyRetries  = 3
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
	for round := 0; round < verifyRetries; round++ {
		if t.stopped.Load() {
			return
		}
		if t.runVerify() {
			return
		}
		if round+1 < verifyRetries {
			t.logf("dogrulama tekrar denenecek")
			if t.sleepStoppable(verifyRetryGap) {
				return
			}
		}
	}
}

func (t *Tunnel) sleepStoppable(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	step := 250 * time.Millisecond
	elapsed := time.Duration(0)
	for elapsed < d {
		if t.stopped.Load() {
			return true
		}
		time.Sleep(step)
		elapsed += step
	}
	return t.stopped.Load()
}

func (t *Tunnel) runVerify() bool {
	ips := t.resolveVerifyIPs()
	if len(ips) == 0 {
		t.logf("dogrulama: dns yaniti yok")
		return false
	}

	best := strategy{}
	bestScore := -1
	for _, s := range strategyOrder(t.strategyID()) {
		if t.stopped.Load() {
			return true
		}
		score := t.probeStrategy(ips, s)
		if score > bestScore {
			bestScore = score
			best = s
		}
		if score >= verifyAccept {
			t.setStrategyID(s.ID)
			t.logf("strateji secildi: %s (%d/%d)", s.ID, score, verifyProbes)
			return true
		}
	}

	if bestScore >= verifyKeep {
		t.setStrategyID(best.ID)
		t.logf("en iyi strateji: %s (%d/%d)", best.ID, bestScore, verifyProbes)
		return true
	}

	t.logf("dogrulama: uygun strateji bulunamadi")
	return false
}

func (t *Tunnel) resolveVerifyIPs() []net.IP {
	for attempt := 0; attempt < 4; attempt++ {
		if t.stopped.Load() {
			return nil
		}
		time.Sleep(verifySettle)
		ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout*2)
		resolved, err := t.resolver.resolveA(ctx, verifyHost)
		cancel()
		if err == nil && len(resolved) > 0 {
			return resolved
		}
	}
	return nil
}

func (t *Tunnel) probeStrategy(ips []net.IP, s strategy) int {
	successes := 0
	for i := 0; i < verifyProbes; i++ {
		if t.stopped.Load() {
			return successes
		}
		if t.probe(ips, s) {
			successes++
		}
	}
	return successes
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
