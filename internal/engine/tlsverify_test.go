package engine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testLeaf(t *testing.T, name string, notAfter time.Time) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cert
}

func stubChainVerifier(t *testing.T, result error) *int {
	t.Helper()
	calls := 0
	previous := verifyChainFn
	verifyChainFn = func(serverName string, certs []*x509.Certificate, now time.Time) error {
		calls++
		return result
	}
	verifiedLeafMu.Lock()
	clear(verifiedLeaves)
	verifiedLeafMu.Unlock()
	t.Cleanup(func() {
		verifyChainFn = previous
		verifiedLeafMu.Lock()
		clear(verifiedLeaves)
		verifiedLeafMu.Unlock()
	})
	return &calls
}

func TestProbeVerificationRunsTheChainOncePerLeaf(t *testing.T) {
	calls := stubChainVerifier(t, nil)
	leaf := testLeaf(t, "discord.com", time.Now().Add(24*time.Hour))
	now := time.Now()

	for i := 0; i < 5; i++ {
		if err := verifyProbePeer("discord.com", []*x509.Certificate{leaf}, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("handshake %d: %v", i, err)
		}
	}
	if *calls != 1 {
		t.Fatalf("chain verified %d times, want 1", *calls)
	}

	if err := verifyProbePeer("discord.com", []*x509.Certificate{leaf}, now.Add(verifiedLeafTTL+time.Second)); err != nil {
		t.Fatalf("after ttl: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("chain verified %d times after the ttl, want 2", *calls)
	}
}

func TestProbeVerificationNeverTrustsACachedLeafForAnotherName(t *testing.T) {
	calls := stubChainVerifier(t, nil)
	leaf := testLeaf(t, "discord.com", time.Now().Add(24*time.Hour))
	now := time.Now()

	if err := verifyProbePeer("discord.com", []*x509.Certificate{leaf}, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	verifyChainFn = func(serverName string, certs []*x509.Certificate, now time.Time) error {
		*calls++
		return certs[0].VerifyHostname(serverName)
	}
	if err := verifyProbePeer("roblox.com", []*x509.Certificate{leaf}, now); err == nil {
		t.Fatal("a leaf verified for discord.com was accepted for roblox.com")
	}
	if *calls != 2 {
		t.Fatalf("name mismatch skipped full verification (%d calls)", *calls)
	}
}

func TestProbeVerificationRechecksAnExpiredLeaf(t *testing.T) {
	calls := stubChainVerifier(t, nil)
	now := time.Now()
	leaf := testLeaf(t, "discord.com", now.Add(time.Minute))

	if err := verifyProbePeer("discord.com", []*x509.Certificate{leaf}, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	verifyChainFn = func(serverName string, certs []*x509.Certificate, at time.Time) error {
		*calls++
		return errors.New("expired")
	}
	if err := verifyProbePeer("discord.com", []*x509.Certificate{leaf}, now.Add(2*time.Minute)); err == nil {
		t.Fatal("an expired leaf was accepted from the cache")
	}
}

func TestProbeVerificationDoesNotCacheFailures(t *testing.T) {
	calls := stubChainVerifier(t, errors.New("unknown authority"))
	leaf := testLeaf(t, "discord.com", time.Now().Add(24*time.Hour))

	for i := 0; i < 3; i++ {
		err := verifyProbePeer("discord.com", []*x509.Certificate{leaf}, time.Now())
		var verifyErr *tls.CertificateVerificationError
		if !errors.As(err, &verifyErr) {
			t.Fatalf("attempt %d: got %v, want a certificate verification error", i, err)
		}
	}
	if *calls != 3 {
		t.Fatalf("chain verified %d times, want every failed attempt to re-verify", *calls)
	}
}

func TestVerifiedLeafCacheStaysBounded(t *testing.T) {
	stubChainVerifier(t, nil)
	now := time.Now()
	for i := 0; i < verifiedLeafMax*3; i++ {
		rememberVerifiedLeaf(leafKey{byte(i), byte(i >> 8)}, now)
	}
	verifiedLeafMu.Lock()
	size := len(verifiedLeaves)
	verifiedLeafMu.Unlock()
	if size > verifiedLeafMax {
		t.Fatalf("cache holds %d leaves, limit is %d", size, verifiedLeafMax)
	}
}

func TestProbeTLSConfigRejectsUntrustedServers(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	addr := server.Listener.Addr().String()

	verifiedLeafMu.Lock()
	clear(verifiedLeaves)
	verifiedLeafMu.Unlock()

	if conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, probeTLSConfig("example.com")); err == nil {
		conn.Close()
		t.Fatal("a self-signed server passed probe verification")
	}

	stubChainVerifier(t, nil)
	verifyChainFn = func(serverName string, certs []*x509.Certificate, now time.Time) error {
		return certs[0].VerifyHostname(serverName)
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, probeTLSConfig("example.com"))
	if err != nil {
		t.Fatalf("trusted handshake: %v", err)
	}
	conn.Close()

	if conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, probeTLSConfig("discord.com")); err == nil {
		conn.Close()
		t.Fatal("a cached leaf was accepted for a name it does not cover")
	}
}
