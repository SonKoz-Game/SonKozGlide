package glide

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	dohQueryTimeout = 4 * time.Second
	dohHedgeDelay   = 300 * time.Millisecond
	dohDialTimeout  = 5 * time.Second
	dnsMessageMax   = 65535
	dnsCacheTTL     = 5 * time.Minute
)

type resolver struct {
	upstreams []string
	client    *http.Client
	logger    Logger
	mu        sync.Mutex
	cache     map[string]cacheEntry
}

type cacheEntry struct {
	reply     []byte
	expiresAt time.Time
}

func newResolver(upstreams []string, logger Logger) *resolver {
	if len(upstreams) == 0 {
		upstreams = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	}
	return &resolver{
		upstreams: upstreams,
		logger:    logger,
		cache:     make(map[string]cacheEntry),
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:               nil,
				DialContext:         (&net.Dialer{Timeout: dohDialTimeout, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout: dohDialTimeout,
				ForceAttemptHTTP2:   true,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (r *resolver) close() {
	r.client.CloseIdleConnections()
}

func (r *resolver) exchange(ctx context.Context, query []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, dohQueryTimeout)
	defer cancel()

	results := make(chan []byte, len(r.upstreams))
	errs := make(chan error, len(r.upstreams))
	launch := func(endpoint string) {
		reply, err := r.post(ctx, endpoint, query)
		if err != nil {
			errs <- err
			return
		}
		results <- reply
	}

	go launch(r.upstreams[0])
	hedge := time.NewTimer(dohHedgeDelay)
	defer hedge.Stop()

	pending := 1
	remaining := len(r.upstreams) - 1
	var lastErr error = errors.New("dns: yanit yok")
	for pending > 0 {
		select {
		case reply := <-results:
			return reply, nil
		case err := <-errs:
			lastErr = err
			pending--
			if remaining > 0 {
				idx := len(r.upstreams) - remaining
				go launch(r.upstreams[idx])
				remaining--
				pending++
			}
		case <-hedge.C:
			if remaining > 0 {
				idx := len(r.upstreams) - remaining
				go launch(r.upstreams[idx])
				remaining--
				pending++
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (r *resolver) post(ctx context.Context, endpoint string, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(endpoint + ": http " + resp.Status)
	}
	reply, err := io.ReadAll(io.LimitReader(resp.Body, dnsMessageMax+1))
	if err != nil {
		return nil, err
	}
	if len(reply) < 12 || len(reply) > dnsMessageMax {
		return nil, errors.New("dns: gecersiz yanit boyutu")
	}
	return reply, nil
}

func (r *resolver) resolveA(ctx context.Context, name string) ([]net.IP, error) {
	key := "A:" + name
	if ips := r.cachedIPs(key); ips != nil {
		return ips, nil
	}
	query, err := buildQuery(name, dnsmessage.TypeA)
	if err != nil {
		return nil, err
	}
	reply, err := r.exchange(ctx, query)
	if err != nil {
		return nil, err
	}
	ips := parseAnswers(reply)
	if len(ips) > 0 {
		r.storeIPs(key, ips)
	}
	return ips, nil
}

func (r *resolver) cachedIPs(key string) []net.IP {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[key+"#ip"]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	ips := make([]net.IP, 0, len(entry.reply)/16)
	for i := 0; i+16 <= len(entry.reply); i += 16 {
		ip := make(net.IP, 16)
		copy(ip, entry.reply[i:i+16])
		ips = append(ips, ip)
	}
	return ips
}

func (r *resolver) storeIPs(key string, ips []net.IP) {
	buf := make([]byte, 0, len(ips)*16)
	for _, ip := range ips {
		buf = append(buf, ip.To16()...)
	}
	r.mu.Lock()
	r.cache[key+"#ip"] = cacheEntry{reply: buf, expiresAt: time.Now().Add(dnsCacheTTL)}
	r.mu.Unlock()
}

func buildQuery(name string, qtype dnsmessage.Type) ([]byte, error) {
	dn, err := dnsmessage.NewName(ensureDot(name))
	if err != nil {
		return nil, err
	}
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name:  dn,
			Type:  qtype,
			Class: dnsmessage.ClassINET,
		}},
	}
	return msg.Pack()
}

func parseAnswers(reply []byte) []net.IP {
	var parser dnsmessage.Parser
	if _, err := parser.Start(reply); err != nil {
		return nil
	}
	if err := parser.SkipAllQuestions(); err != nil {
		return nil
	}
	var ips []net.IP
	for {
		header, err := parser.AnswerHeader()
		if err != nil {
			break
		}
		switch header.Type {
		case dnsmessage.TypeA:
			rec, err := parser.AResource()
			if err == nil {
				ip := make(net.IP, 4)
				copy(ip, rec.A[:])
				ips = append(ips, ip)
			}
		case dnsmessage.TypeAAAA:
			rec, err := parser.AAAAResource()
			if err == nil {
				ip := make(net.IP, 16)
				copy(ip, rec.AAAA[:])
				ips = append(ips, ip)
			}
		default:
			if err := parser.SkipAnswer(); err != nil {
				return ips
			}
		}
	}
	return ips
}

func ensureDot(name string) string {
	if len(name) == 0 || name[len(name)-1] != '.' {
		return name + "."
	}
	return name
}
