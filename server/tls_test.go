package main

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestGenerateTLSConfigReturnsValidCert(t *testing.T) {
	validity := 2 * time.Hour
	tlsCfg, fingerprint := generateTLSConfig(validity)

	if tlsCfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if len(fingerprint) != 64 { // SHA-256 hex = 32 bytes = 64 chars
		t.Errorf("fingerprint length: got %d, want 64", len(fingerprint))
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}

	leaf := tlsCfg.Certificates[0].Leaf
	if leaf == nil {
		t.Fatal("expected parsed leaf certificate")
	}
	if leaf.Subject.CommonName != "bken" {
		t.Errorf("CN: got %q, want %q", leaf.Subject.CommonName, "bken")
	}

	// Cert should be valid now.
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		t.Errorf("cert not valid at current time: NotBefore=%v NotAfter=%v", leaf.NotBefore, leaf.NotAfter)
	}

	// NotAfter should be within the requested validity window (plus the 1-hour backdating).
	expectedAfter := now.Add(validity)
	if leaf.NotAfter.Before(expectedAfter.Add(-2 * time.Hour)) {
		t.Errorf("NotAfter too early: %v (expected near %v)", leaf.NotAfter, expectedAfter)
	}
}

func TestGenerateTLSConfigUniqueCerts(t *testing.T) {
	_, fp1 := generateTLSConfig(time.Hour)
	_, fp2 := generateTLSConfig(time.Hour)
	if fp1 == fp2 {
		t.Error("two calls should produce different certificates")
	}
}

func TestGenerateTLSConfigSelfSigned(t *testing.T) {
	tlsCfg, _ := generateTLSConfig(time.Hour)
	leaf := tlsCfg.Certificates[0].Leaf

	// The cert should be self-signed (issuer == subject).
	if leaf.Issuer.CommonName != leaf.Subject.CommonName {
		t.Errorf("expected self-signed cert: issuer=%q subject=%q", leaf.Issuer.CommonName, leaf.Subject.CommonName)
	}

	// Should include localhost in DNS names.
	found := false
	for _, name := range leaf.DNSNames {
		if name == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected localhost in DNS names, got %v", leaf.DNSNames)
	}

	// Verify against itself.
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	_, err := leaf.Verify(x509.VerifyOptions{
		DNSName: "localhost",
		Roots:   pool,
	})
	if err != nil {
		t.Errorf("self-verification failed: %v", err)
	}
}
