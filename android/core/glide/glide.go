package glide

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

type Logger interface {
	Log(line string)
}

type Options struct {
	DohServers    string
	Domains       string
	Strategy      string
	Mtu           int
	DnsV4         string
	DnsV6         string
	VerifyOnStart bool
}

type Tunnel struct {
	stack    *tunStack
	resolver *resolver
	rules    *ruleSet
	logger   Logger
	dnsV4    netip.Addr
	dnsV6    netip.Addr
	strategy atomic.Value
	served   atomic.Uint64
	stopped  atomic.Bool
	mu       sync.Mutex
}

func defaultOptions() *Options {
	return &Options{
		DohServers:    "https://1.1.1.1/dns-query,https://8.8.8.8/dns-query",
		Strategy:      "",
		Mtu:           1500,
		DnsV4:         "10.111.0.53",
		DnsV6:         "fd00:5100:6b6f::53",
		VerifyOnStart: true,
	}
}

func Start(fd int, opts *Options, logger Logger) (*Tunnel, error) {
	if fd <= 0 {
		return nil, errors.New("gecersiz tun")
	}
	base := defaultOptions()
	if opts != nil {
		mergeOptions(base, opts)
	}
	rules, err := parseRules(base.Domains)
	if err != nil {
		return nil, err
	}
	dnsV4, err := netip.ParseAddr(base.DnsV4)
	if err != nil {
		return nil, errors.New("gecersiz dns v4")
	}
	dnsV6, err := netip.ParseAddr(base.DnsV6)
	if err != nil {
		return nil, errors.New("gecersiz dns v6")
	}
	res := newResolver(splitList(base.DohServers), logger)
	t := &Tunnel{
		resolver: res,
		rules:    rules,
		logger:   logger,
		dnsV4:    dnsV4,
		dnsV6:    dnsV6,
	}
	t.setStrategyID(normalizeStrategy(base.Strategy))
	st, err := startTunStack(fd, base, t)
	if err != nil {
		return nil, err
	}
	t.stack = st
	t.logf("tunel acildi, kural %d alan adi", rules.count())
	if base.VerifyOnStart {
		go t.verify()
	}
	return t, nil
}

func (t *Tunnel) Stop() {
	if t.stopped.Swap(true) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stack != nil {
		t.stack.close()
	}
	t.resolver.close()
	t.logf("tunel kapandi")
}

func (t *Tunnel) Status() string {
	if t.stopped.Load() {
		return "kapali"
	}
	return fmt.Sprintf("acik strateji=%s akis=%d", t.strategyID(), t.served.Load())
}

func (t *Tunnel) ActiveStrategy() string {
	return t.strategyID()
}

func (t *Tunnel) strategyID() string {
	if v, ok := t.strategy.Load().(string); ok {
		return v
	}
	return defaultStrategyID
}

func (t *Tunnel) setStrategyID(id string) {
	t.strategy.Store(id)
}

func (t *Tunnel) logf(format string, args ...any) {
	if t.logger == nil {
		return
	}
	t.logger.Log(time.Now().Format("15:04:05 ") + fmt.Sprintf(format, args...))
}

func mergeOptions(dst *Options, src *Options) {
	if src.DohServers != "" {
		dst.DohServers = src.DohServers
	}
	if src.Domains != "" {
		dst.Domains = src.Domains
	}
	if src.Strategy != "" {
		dst.Strategy = src.Strategy
	}
	if src.Mtu > 0 {
		dst.Mtu = src.Mtu
	}
	if src.DnsV4 != "" {
		dst.DnsV4 = src.DnsV4
	}
	if src.DnsV6 != "" {
		dst.DnsV6 = src.DnsV6
	}
	dst.VerifyOnStart = src.VerifyOnStart
}
