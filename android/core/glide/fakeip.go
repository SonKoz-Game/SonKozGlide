package glide

import (
	"errors"
	"net/netip"
	"sync"
)

type fakePool struct {
	mu       sync.Mutex
	v4base   netip.Addr
	v6base   netip.Addr
	nextV4   uint32
	nextV6   uint32
	byName   map[string]fakePair
	byAddrV4 map[netip.Addr]string
	byAddrV6 map[netip.Addr]string
}

type fakePair struct {
	v4 netip.Addr
	v6 netip.Addr
}

func newFakePool(v4base, v6base string) (*fakePool, error) {
	a4, err := netip.ParseAddr(v4base)
	if err != nil || !a4.Is4() {
		return nil, errors.New("gecersiz v4 taban")
	}
	a6, err := netip.ParseAddr(v6base)
	if err != nil || !a6.Is6() {
		return nil, errors.New("gecersiz v6 taban")
	}
	return &fakePool{
		v4base:   a4,
		v6base:   a6,
		nextV4:   1,
		nextV6:   1,
		byName:   make(map[string]fakePair),
		byAddrV4: make(map[netip.Addr]string),
		byAddrV6: make(map[netip.Addr]string),
	}, nil
}

func (p *fakePool) assign(name string) fakePair {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pair, ok := p.byName[name]; ok {
		return pair
	}
	v4 := addOffset4(p.v4base, p.nextV4)
	v6 := addOffset6(p.v6base, p.nextV6)
	p.nextV4++
	p.nextV6++
	pair := fakePair{v4: v4, v6: v6}
	p.byName[name] = pair
	p.byAddrV4[v4] = name
	p.byAddrV6[v6] = name
	return pair
}

func (p *fakePool) lookup(addr netip.Addr) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if addr.Is4() {
		name, ok := p.byAddrV4[addr]
		return name, ok
	}
	name, ok := p.byAddrV6[addr]
	return name, ok
}

func addOffset4(base netip.Addr, n uint32) netip.Addr {
	b := base.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v += n
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func addOffset6(base netip.Addr, n uint32) netip.Addr {
	b := base.As16()
	b[15] = byte(n)
	b[14] = byte(n >> 8)
	return netip.AddrFrom16(b)
}
