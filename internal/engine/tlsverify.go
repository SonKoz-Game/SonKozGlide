package engine

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"sync"
	"time"
)

const (
	verifiedLeafTTL = time.Hour
	verifiedLeafMax = 64
)

type leafKey [sha256.Size]byte

var (
	verifiedLeafMu sync.Mutex
	verifiedLeaves = make(map[leafKey]time.Time)
	verifyChainFn  = verifyChain
)

func probeTLSConfig(serverName string) *tls.Config {
	return &tls.Config{
		ServerName:         serverName,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			return verifyProbePeer(serverName, state.PeerCertificates, time.Now())
		},
	}
}

func verifyProbePeer(serverName string, certs []*x509.Certificate, now time.Time) error {
	if len(certs) == 0 {
		return errors.New("tls: server sent no certificate")
	}

	leaf := certs[0]
	key := leafKey(sha256.Sum256(leaf.Raw))
	if leafRecentlyVerified(key, now) && now.Before(leaf.NotAfter) && leaf.VerifyHostname(serverName) == nil {
		return nil
	}

	if err := verifyChainFn(serverName, certs, now); err != nil {
		return &tls.CertificateVerificationError{UnverifiedCertificates: certs, Err: err}
	}
	rememberVerifiedLeaf(key, now)
	return nil
}

func verifyChain(serverName string, certs []*x509.Certificate, now time.Time) error {
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	_, err := certs[0].Verify(x509.VerifyOptions{
		DNSName:       serverName,
		Intermediates: intermediates,
		CurrentTime:   now,
	})
	return err
}

func leafRecentlyVerified(key leafKey, now time.Time) bool {
	verifiedLeafMu.Lock()
	defer verifiedLeafMu.Unlock()
	until, ok := verifiedLeaves[key]
	return ok && now.Before(until)
}

func rememberVerifiedLeaf(key leafKey, now time.Time) {
	verifiedLeafMu.Lock()
	defer verifiedLeafMu.Unlock()
	if len(verifiedLeaves) >= verifiedLeafMax {
		for k, until := range verifiedLeaves {
			if !now.Before(until) {
				delete(verifiedLeaves, k)
			}
		}
		if len(verifiedLeaves) >= verifiedLeafMax {
			clear(verifiedLeaves)
		}
	}
	verifiedLeaves[key] = now.Add(verifiedLeafTTL)
}
