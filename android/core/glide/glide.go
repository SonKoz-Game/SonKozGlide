package glide

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Logger interface {
	Log(line string)
}

type Options struct {
	DohServers   string
	Domains      string
	Strategy     string
	Mtu          int
	FakeV4Base   string
	FakeV6Base   string
	DnsFakeV4    string
	DnsFakeV6    string
	VerifyOnStart bool
}

type Tunnel struct {
	stack    *tunStack
	resolver *resolver
	rules    *ruleSet
	fakePool *fakePool
	logger   Logger
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
		FakeV4Base:    "198.18.1.0",
		FakeV6Base:    "fd61:7363:6b6f:1::",
		DnsFakeV4:     "198.18.255.254",
		DnsFakeV6:     "fd61:7363:6b6f:ffff::ffff",
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
	pool, err := newFakePool(base.FakeV4Base, base.FakeV6Base)
	if err != nil {
		return nil, err
	}
	res := newResolver(splitList(base.DohServers), logger)
	t := &Tunnel{
		resolver: res,
		rules:    rules,
		fakePool: pool,
		logger:   logger,
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
	if src.FakeV4Base != "" {
		dst.FakeV4Base = src.FakeV4Base
	}
	if src.FakeV6Base != "" {
		dst.FakeV6Base = src.FakeV6Base
	}
	if src.DnsFakeV4 != "" {
		dst.DnsFakeV4 = src.DnsFakeV4
	}
	if src.DnsFakeV6 != "" {
		dst.DnsFakeV6 = src.DnsFakeV6
	}
	dst.VerifyOnStart = src.VerifyOnStart
}
